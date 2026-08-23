package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		email    string
		user     string
		want     error
	}{
		{"strong passphrase", "correct-horse-battery", "a@b.com", "Ada Lovelace", nil},
		{"strong mixed", "Tr0ubad0ur&3x", "a@b.com", "Ada Lovelace", nil},
		{"digit run inside long passphrase", "correcthorsebattery1234", "a@b.com", "Ada Lovelace", nil},
		{"exactly minimum length", "hj4kqp2z", "a@b.com", "Ada Lovelace", nil},

		{"too short", "sys1234", "a@b.com", "Ada Lovelace", ErrPasswordTooShort},
		{"empty", "", "a@b.com", "Ada Lovelace", ErrPasswordTooShort},
		{"over bcrypt limit", strings.Repeat("aB3$", 19), "a@b.com", "Ada Lovelace", ErrPasswordTooLong},

		{"common password", "password123", "a@b.com", "Ada Lovelace", ErrPasswordCommon},
		{"common uppercased", "PassWord123", "a@b.com", "Ada Lovelace", ErrPasswordCommon},
		{"common keyboard walk", "1q2w3e4r", "a@b.com", "Ada Lovelace", ErrPasswordCommon},

		{"prefix plus digit sequence", "sys12345", "a@b.com", "Ada Lovelace", ErrPasswordSequence},
		{"descending sequence", "zyxwvuts", "a@b.com", "Ada Lovelace", ErrPasswordSequence},
		{"all repeated", "aaaaaaaa", "a@b.com", "Ada Lovelace", ErrPasswordSequence},
		{"too few distinct", "abababab", "a@b.com", "Ada Lovelace", ErrPasswordSequence},

		{"contains email local part", "tushar-queue-x", "tushar@mail.com", "Ada Lovelace", ErrPasswordPersonal},
		{"contains given name", "adaSecretRunner", "a@b.com", "Ada Lovelace", ErrPasswordPersonal},
		{"short name token ignored", "quietMeadow42", "a@b.com", "Al Lovelace", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidatePassword(tt.password, tt.email, tt.user)
			if !errors.Is(got, tt.want) {
				t.Fatalf("ValidatePassword(%q) = %v, want %v", tt.password, got, tt.want)
			}
		})
	}
}

func TestValidatePasswordRejectsBcryptOverflowBoundary(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("aB3$", 18), "a@b.com", "Ada Lovelace"); err != nil {
		t.Fatalf("72 bytes must be accepted, got %v", err)
	}
	if err := ValidatePassword(strings.Repeat("aB3$", 18)+"x", "a@b.com", "Ada Lovelace"); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("73 bytes must be rejected, got %v", err)
	}
}

func TestCollapseRuns(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"sys12345", "sys"},
		{"abcd", ""},
		{"abc", "abc"},
		{"aaaa", ""},
		{"correcthorsebattery1234", "correcthorsebattery"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := collapseRuns(tt.in); got != tt.want {
			t.Errorf("collapseRuns(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
