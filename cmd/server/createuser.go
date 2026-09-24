package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/DhanushRamesh/personal-assistant/internal/auth"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
)

// createUser : Creates a user from the terminal.
//
// There is no endpoint for this. An endpoint that creates the first user must
// either be open, which lets a stranger claim the assistant, or be guarded by
// a shared secret, which is the same problem one level up. A command run by
// whoever already has the machine avoids both, and is needed exactly once.
func createUser(username string, db *storageDB) error {
	username = chat.NormaliseUsername(username)

	password, err := readPassword()
	if err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	user, err := chat.NewUser(username, hash)
	if err != nil {
		if errors.Is(err, chat.ErrInvalidUsername) {
			return fmt.Errorf("a username must be %d to %d characters of letters, digits, dots, dashes or underscores",
				chat.MinUsernameLen, chat.MaxUsernameLen)
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo := chatmysql.NewRepository(db)
	if err := repo.CreateUser(ctx, user); err != nil {
		if errors.Is(err, chat.ErrUsernameTaken) {
			return fmt.Errorf("the username %q is already taken", username)
		}
		return err
	}

	fmt.Printf("created user %s (%s)\n", user.Username, user.ID)
	fmt.Println("log in from a client with:")
	fmt.Printf("  curl -X POST localhost:8080/v1/auth/login \\\n")
	fmt.Printf("    -H 'Content-Type: application/json' \\\n")
	fmt.Printf("    -d '{\"username\":%q,\"password\":\"...\",\"client_name\":\"my phone\"}'\n", user.Username)
	return nil
}

// readPassword : Reads a password twice from the terminal without echoing it.
func readPassword() (string, error) {
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", errors.New("a password must be typed at a terminal")
	}

	fmt.Print("password: ")
	first, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	fmt.Print("again: ")
	second, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	if !strings.EqualFold(string(first), string(second)) || string(first) != string(second) {
		return "", errors.New("the passwords do not match")
	}
	if err := auth.CheckPasswordPolicy(string(first)); err != nil {
		return "", err
	}
	return string(first), nil
}

// osExitOnError : Reports a command failure plainly and stops.
func osExitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
