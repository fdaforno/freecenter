package esxi

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/vim25/types"

	"hostwarden/internal/model"
)

// Real-time statistics are sampled by ESXi every 20 seconds and kept for one hour.
const (
	perfInterval   = 20
	perfMaxSamples = 180
)

// perfMetrics are the counters shown in the Monitor tab. ESXi reports
// percentages in hundredths of a percent.
var perfMetrics = []struct {
	counter, key, label, unit string
	scale                     float64
}{
	{"cpu.usage.average", "cpu", "CPU usage", "%", 0.01},
	{"mem.usage.average", "memory", "Memory usage", "%", 0.01},
	{"disk.usage.average", "disk", "Disk usage", "KBps", 1},
	{"net.usage.average", "network", "Network usage", "KBps", 1},
}

// Performance returns the real-time performance of the host (vmID empty) or
// of one VM. A powered-off VM has no samples: the result is then empty.
func (c *Client) Performance(ctx context.Context, vmID string) (*model.PerfData, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	ref, err := c.monitorTarget(ctx, vmID)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(perfMetrics))
	for i, m := range perfMetrics {
		names[i] = m.counter
	}
	pm := performance.NewManager(c.vc)
	spec := types.PerfQuerySpec{
		MaxSample:  perfMaxSamples,
		IntervalId: perfInterval,
		MetricId:   []types.PerfMetricId{{Instance: ""}}, // aggregate over all instances
	}
	out := &model.PerfData{IntervalSeconds: perfInterval, Times: []time.Time{}, Series: []model.PerfSeries{}}
	sample, err := pm.SampleByName(ctx, spec, names, []types.ManagedObjectReference{ref})
	if err != nil {
		return nil, err
	}
	entities, err := pm.ToMetricSeries(ctx, sample)
	if err != nil {
		return nil, err
	}
	if len(entities) == 0 {
		return out, nil
	}
	e := entities[0]
	for _, s := range e.SampleInfo {
		out.Times = append(out.Times, s.Timestamp)
	}
	for _, m := range perfMetrics {
		for _, v := range e.Value {
			if v.Name != m.counter || v.Instance != "" {
				continue
			}
			vals := make([]float64, len(v.Value))
			for i, x := range v.Value {
				// ESXi reports -1 for a sample it could not collect.
				vals[i] = max(0, float64(x)*m.scale)
			}
			out.Series = append(out.Series, model.PerfSeries{Key: m.key, Label: m.label, Unit: m.unit, Values: vals})
		}
	}
	return out, nil
}

func (c *Client) monitorTarget(ctx context.Context, vmID string) (types.ManagedObjectReference, error) {
	if vmID == "" {
		return c.hostSystemRef(ctx)
	}
	if _, err := c.retrieveVM(ctx, vmID, []string{"name"}); err != nil {
		return types.ManagedObjectReference{}, err
	}
	return vmRef(vmID), nil
}

// Events returns the newest entries of the host's event log, newest first:
// all of them (vmID empty) or those about one VM.
func (c *Client) Events(ctx context.Context, vmID string, limit int) ([]model.Event, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	filter := types.EventFilterSpec{}
	if vmID != "" {
		ref, err := c.monitorTarget(ctx, vmID)
		if err != nil {
			return nil, err
		}
		filter.Entity = &types.EventFilterSpecByEntity{Entity: ref, Recursion: types.EventFilterSpecRecursionOptionSelf}
	}
	// Standalone ESXi does not implement QueryEvents: read the latest page of a
	// history collector instead, as the Host Client does.
	m := event.NewManager(c.vc)
	coll, err := m.CreateCollectorForEvents(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer coll.Destroy(context.WithoutCancel(ctx))
	if limit > 0 {
		// Best effort: the default page is smaller, but still the newest events.
		_ = coll.SetPageSize(ctx, int32(limit))
	}
	events, err := coll.LatestPage(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Event, 0, len(events))
	for _, be := range events {
		e := be.GetEvent()
		ev := model.Event{Time: e.CreatedTime, Message: strings.TrimSpace(e.FullFormattedMessage), User: e.UserName}
		if e.Vm != nil {
			ev.Target = e.Vm.Name
		}
		if cat, err := m.EventCategory(ctx, be); err == nil {
			ev.Category = cat
		}
		if ev.Category == "" {
			ev.Category = "info"
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
