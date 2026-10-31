package domain

import "testing"

func TestValidPIN(t *testing.T) {
	for pin, want := range map[string]bool{
		"": false, "123": false, "1234": true, "12345678": true, "123456789": false,
		"12a4": false, "12 4": false, "١٢٣٤": false, "0000": true,
	} {
		if got := ValidPIN(pin); got != want {
			t.Errorf("ValidPIN(%q) = %v, want %v", pin, got, want)
		}
	}
}
