import { Injectable, inject } from '@angular/core';

import { ApiService } from './api.service';
import { Datastore } from './models';

/** Conventional folder for installation media on every datastore. */
const ISO_FOLDER = '/ISO';

/** Finds the ISO images a CD/DVD drive can be backed by. */
@Injectable({ providedIn: 'root' })
export class IsoService {
  private readonly api = inject(ApiService);

  /** Datastore paths ("[ds] ISO/file.iso") of the images in the ISO folder of each datastore. */
  async list(hostId: string, datastores: Datastore[]): Promise<string[]> {
    const perStore = await Promise.all(
      datastores
        .filter((d) => d.accessible)
        .map(async (d) => {
          // A datastore without the folder is normal, not an error.
          const files = await this.api.files(hostId, d.name, ISO_FOLDER).catch(() => []);
          return files.filter((f) => !f.is_dir && /\.iso$/i.test(f.name)).map((f) => `[${d.name}] ISO/${f.name}`);
        }),
    );
    return perStore.flat().sort();
  }
}
