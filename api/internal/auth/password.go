package auth

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MinPasswordRunes = 8
	MaxPasswordBytes = 72
	minResidualRunes = 6
	minDistinctRunes = 4
	minRunLength     = 4
	minContextToken  = 3
)

var (
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 72 bytes")
	ErrPasswordCommon   = errors.New("this password is among the most commonly breached, pick something less predictable")
	ErrPasswordSequence = errors.New("password relies on sequences or repeated characters, pick something less predictable")
	ErrPasswordPersonal = errors.New("password must not contain your name or email address")
)

var commonPasswords = map[string]struct{}{
	"password": {}, "password1": {}, "password12": {}, "password123": {}, "password1234": {},
	"passw0rd": {}, "p@ssw0rd": {}, "p@ssword": {}, "passwd": {}, "pass1234": {},
	"12345678": {}, "123456789": {}, "1234567890": {}, "123123123": {}, "112233445": {},
	"qwertyui": {}, "qwerty123": {}, "qwertyuiop": {}, "asdfghjkl": {}, "zxcvbnm123": {},
	"iloveyou": {}, "princess": {}, "sunshine": {}, "football": {}, "baseball": {},
	"welcome1": {}, "welcome123": {}, "admin123": {}, "administrator": {}, "root1234": {},
	"letmein1": {}, "letmein123": {}, "trustno1": {}, "superman": {}, "batman123": {},
	"monkey12": {}, "dragon123": {}, "master123": {}, "shadow123": {}, "michael1": {},
	"abc12345": {}, "abcd1234": {}, "a1b2c3d4": {}, "1q2w3e4r": {}, "1qaz2wsx": {},
	"qazwsxedc": {}, "zaq12wsx": {}, "q1w2e3r4": {}, "asdf1234": {}, "test1234": {},
	"changeme": {}, "changeme123": {}, "default123": {}, "temp1234": {}, "secret123": {},
	"jennifer": {}, "jordan23": {}, "hunter123": {}, "computer": {}, "internet": {},
	"whatever": {}, "starwars": {}, "pokemon123": {}, "liverpool": {}, "chelsea1": {},
	"samsung1": {}, "google123": {}, "facebook1": {}, "linkedin": {}, "twitter1": {},
	"summer2024": {}, "summer2025": {}, "winter2024": {}, "spring2024": {}, "autumn2024": {},
	"january1": {}, "december1": {}, "november1": {}, "birthday1": {}, "freedom1": {},
	"nintendo": {}, "playstation": {}, "minecraft": {}, "fortnite1": {}, "gamer123": {},
	"school123": {}, "student1": {}, "teacher1": {}, "company1": {}, "business1": {},
	"database": {}, "postgres": {}, "redis123": {}, "docker123": {}, "developer": {},
	"security": {}, "manager1": {}, "service1": {}, "system123": {}, "backup123": {},
}

func ValidatePassword(password, email, name string) error {
	if utf8.RuneCountInString(password) < MinPasswordRunes {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}

	lower := strings.ToLower(password)
	if _, found := commonPasswords[lower]; found {
		return ErrPasswordCommon
	}
	if containsPersonalData(lower, email, name) {
		return ErrPasswordPersonal
	}
	if utf8.RuneCountInString(collapseRuns(lower)) < minResidualRunes {
		return ErrPasswordSequence
	}
	if distinctRunes(lower) < minDistinctRunes {
		return ErrPasswordSequence
	}
	return nil
}

func collapseRuns(s string) string {
	runes := []rune(s)
	var kept []rune
	for start := 0; start < len(runes); {
		end := start + 1
		for end < len(runes) {
			prev, cur := runes[end-1], runes[end]
			if cur != prev && cur != prev+1 && cur != prev-1 {
				break
			}
			end++
		}
		if end-start < minRunLength {
			kept = append(kept, runes[start:end]...)
		}
		start = end
	}
	return string(kept)
}

func distinctRunes(s string) int {
	seen := make(map[rune]struct{}, len(s))
	for _, r := range s {
		seen[r] = struct{}{}
	}
	return len(seen)
}

func containsPersonalData(lower, email, name string) bool {
	tokens := strings.Fields(strings.ToLower(name))
	if local, _, found := strings.Cut(strings.ToLower(email), "@"); found && local != "" {
		tokens = append(tokens, local)
	}
	for _, token := range tokens {
		if utf8.RuneCountInString(token) >= minContextToken && strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
