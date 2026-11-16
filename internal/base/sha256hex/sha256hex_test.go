package sha256hex

import "testing"

func TestDigestFormatsLowercaseHex(t *testing.T) {
	t.Parallel()
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := Digest(nil); got != want {
		t.Fatalf("Digest(nil) = %q, want %q", got, want)
	}
	if !IsDigest(want) {
		t.Fatalf("IsDigest(%q) = false, want true", want)
	}
}

func TestIsDigestRejectsOtherText(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"",
		"E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85g",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8550",
	} {
		if IsDigest(text) {
			t.Errorf("IsDigest(%q) = true, want false", text)
		}
	}
}
