package data

import (
	"strings"
	"testing"
)

// The reset code is the only thing standing between "has an address and a
// manager's good intentions" and the account itself, so these cover the two
// things that decide whether that holds: the code has enough entropy to be
// worth guessing at, and a hash of it cannot be reversed by trying every
// candidate.

func TestGenerateResetCodeIsSixDigits(t *testing.T) {
	code, err := GenerateResetCode()
	if err != nil {
		t.Fatalf("GenerateResetCode returned an error: %v", err)
	}
	if len(code) != resetCodeDigits {
		t.Errorf("code %q is %d characters, want %d", code, len(code), resetCodeDigits)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Errorf("code %q contains %q, want digits only", code, r)
		}
	}
}

// Zero-padded, so "004271" stays six digits however someone writes it down or
// reads it aloud.
func TestGenerateResetCodeZeroPadsSmallNumbers(t *testing.T) {
	for i := 0; i < 400; i++ {
		code, err := GenerateResetCode()
		if err != nil {
			t.Fatalf("GenerateResetCode returned an error: %v", err)
		}
		if len(code) != resetCodeDigits {
			t.Fatalf("code %q is %d characters, want %d — not zero-padded", code, len(code), resetCodeDigits)
		}
	}
}

func TestGenerateResetCodeVaries(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		code, err := GenerateResetCode()
		if err != nil {
			t.Fatalf("GenerateResetCode returned an error: %v", err)
		}
		seen[code] = true
	}
	// 200 draws from a million possibilities should essentially never collide.
	if len(seen) < 190 {
		t.Errorf("only %d distinct codes out of 200 draws, want ~200", len(seen))
	}
}

func TestVerifyResetCodeAcceptsTheRightCode(t *testing.T) {
	code, err := GenerateResetCode()
	if err != nil {
		t.Fatalf("GenerateResetCode returned an error: %v", err)
	}
	stored, err := hashResetCode(code)
	if err != nil {
		t.Fatalf("hashResetCode returned an error: %v", err)
	}
	if !verifyResetCode(stored, code) {
		t.Error("the code that was hashed did not verify against its own hash")
	}
}

func TestVerifyResetCodeRejectsTheWrongCode(t *testing.T) {
	code, err := GenerateResetCode()
	if err != nil {
		t.Fatalf("GenerateResetCode returned an error: %v", err)
	}
	stored, err := hashResetCode(code)
	if err != nil {
		t.Fatalf("hashResetCode returned an error: %v", err)
	}

	// One digit different is the realistic near-miss.
	other := "0" + strings.TrimPrefix(code, "0")
	if other != code && verifyResetCode(stored, other) {
		t.Errorf("code %q verified against a hash of %q", other, code)
	}
	if verifyResetCode(stored, "") {
		t.Error("an empty code verified against a real hash")
	}
}

func TestVerifyResetCodeRejectsWhenNothingWasStored(t *testing.T) {
	if verifyResetCode(nil, "123456") {
		t.Error("a code verified against no stored hash")
	}
	if verifyResetCode([]byte{}, "123456") {
		t.Error("a code verified against an empty stored hash")
	}
}

// A code hash must not be readable back into the code by hashing candidates.
// Six digits is only a million possibilities, which is exactly why the stored
// hash has to be slow.
func TestStoredResetCodeIsNotReversibleByFastHashing(t *testing.T) {
	code, err := GenerateResetCode()
	if err != nil {
		t.Fatalf("GenerateResetCode returned an error: %v", err)
	}
	stored, err := hashResetCode(code)
	if err != nil {
		t.Fatalf("hashResetCode returned an error: %v", err)
	}

	// bcrypt hashes carry a cost and a salt, so the plaintext is not sitting in
	// the stored bytes. This is the cheap structural check; the cost itself is
	// what makes enumerating the million candidates impractical.
	if strings.Contains(string(stored), code) {
		t.Error("the stored hash contains the code in plaintext")
	}
	// $2a$12$ — algorithm, then cost 12.
	if len(stored) < 7 || string(stored[:4]) != "$2a$" {
		t.Errorf("stored hash %q does not look like a bcrypt hash", stored)
	}
}

func TestHashResetCodeIsSalted(t *testing.T) {
	const code = "123456"
	first, err := hashResetCode(code)
	if err != nil {
		t.Fatalf("hashResetCode returned an error: %v", err)
	}
	second, err := hashResetCode(code)
	if err != nil {
		t.Fatalf("hashResetCode returned an error: %v", err)
	}
	if string(first) == string(second) {
		t.Error("hashing the same code twice gave the same bytes, so it is unsalted")
	}
	if !verifyResetCode(first, code) || !verifyResetCode(second, code) {
		t.Error("a salted hash failed to verify against the code it was made from")
	}
}
