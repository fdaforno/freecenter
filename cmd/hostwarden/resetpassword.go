package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"hostwarden/internal/store"
)

// minPasswordLen matches what the API asks of a new password.
const minPasswordLen = 8

// resetPassword implements "hostwarden reset-password [username]": the way
// back in when the administrator's password is lost. It asks for the new
// password on the terminal and writes it to the state file; a running server
// keeps its state in memory and must be restarted to see the change.
func resetPassword(storePath, username string) error {
	st, err := store.Open(storePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	var user *store.User
	for _, u := range st.Users() {
		if u.Username == username {
			user = &u
			break
		}
	}
	if user == nil {
		return fmt.Errorf("no user named %q", username)
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("the password is read from a terminal: run this command interactively (docker exec -it ...)")
	}
	ask := func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	password, err := ask("New password for " + username + ": ")
	if err != nil {
		return err
	}
	if len(password) < minPasswordLen {
		return fmt.Errorf("the password must be at least %d characters long", minPasswordLen)
	}
	again, err := ask("Repeat it: ")
	if err != nil {
		return err
	}
	if again != password {
		return errors.New("the two passwords differ")
	}
	if err := st.SetUserPassword(user.ID, password, ""); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Password changed. Restart the Hostwarden server for it to take effect.")
	return nil
}
