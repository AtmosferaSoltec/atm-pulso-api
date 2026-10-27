// Command first-superadmin prints the SQL that inserts the first superadmin. It does not
// touch any database: the owner reads the statement and runs it by hand against the
// database, so the backend needs no seed or "create superadmin" logic at all.
//
//	go run ./scripts/first-superadmin
//
// It asks for the DNI, the full name and the password (hidden, twice) and writes the
// statement to stdout. Prompts go to stderr, so the output can be piped:
//
//	go run ./scripts/first-superadmin | ssh atmosfera \
//	  'docker exec -i atm-postgres psql -v ON_ERROR_STOP=1 -U postgres -d pulso'
//
// The password never leaves this machine: only its argon2id hash is in the statement.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/security"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	reader := bufio.NewReader(os.Stdin)
	document, err := ask(reader, "DNI (8 digits): ")
	if err != nil {
		return err
	}
	fullName, err := ask(reader, "Full name: ")
	if err != nil {
		return err
	}
	password, err := askPassword("Password (hidden, at least 8 characters): ")
	if err != nil {
		return err
	}
	again, err := askPassword("Repeat the password: ")
	if err != nil {
		return err
	}
	if password != again {
		return errors.New("the passwords do not match")
	}

	hash, err := security.Argon2idHasher{}.Hash(password)
	if err != nil {
		return err
	}
	statement, err := Statement(document, fullName, hash)
	if err != nil {
		return err
	}
	fmt.Print(statement)
	fmt.Fprintln(os.Stderr, "\nSQL printed. Run it against the pulso database; nothing was written by this tool.")
	return nil
}

// Statement builds the INSERT after validating what the application itself would require
// of a superadmin: a DNI, a real name and a password that meets the policy.
func Statement(document, fullName, passwordHash string) (string, error) {
	document = strings.TrimSpace(document)
	name := domain.NormalizeFullName(fullName)
	if !domain.ValidDocumentNumber(document) {
		return "", errors.New("the DNI must have exactly 8 digits")
	}
	if _, _, err := domain.ProfileChanges(name, ""); err != nil {
		return "", errors.New("the name must have between 3 and 120 characters")
	}
	if !strings.HasPrefix(passwordHash, "$argon2id$") {
		return "", errors.New("not an argon2id hash")
	}
	return fmt.Sprintf(`SET search_path TO pulso;
INSERT INTO users (id, document_number, full_name, role, password_hash)
VALUES (gen_random_uuid(), %s, %s, 'superadmin', %s);
`, quote(document), quote(name), quote(passwordHash)), nil
}

// quote makes a SQL string literal. The values are validated first; doubling the quote is
// the standard escape and keeps a name such as O'Brien from breaking the statement.
func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func ask(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func askPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read the password (run it in a terminal): %w", err)
	}
	password := string(raw)
	if err := domain.NewPassword("", password); err != nil {
		return "", errors.New("the password must have at least 8 characters")
	}
	return password, nil
}
