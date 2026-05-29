package capture

import (
	"image"
	"testing"
)

func TestEncodePNG(t *testing.T) {
	// given: a simple RGBA image
	img := image.NewRGBA(image.Rect(0, 0, 10, 20))

	// when: encoding to PNG
	data, err := EncodePNG(img)

	// then: no error and valid PNG data
	if err != nil {
		t.Fatalf("EncodePNG() unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("EncodePNG() returned empty data")
	}

	// verify it is valid PNG by decoding it
	_, err = DecodePNG(data)
	if err != nil {
		t.Fatalf("EncodePNG output not decodable: %v", err)
	}
}

func TestValidatePNG_Success(t *testing.T) {
	// given: a valid PNG image with known dimensions
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	data, err := EncodePNG(img)
	if err != nil {
		t.Fatalf("EncodePNG() unexpected error: %v", err)
	}

	// when: validating with correct dimensions
	err = ValidatePNG(data, 100, 50)

	// then: no error
	if err != nil {
		t.Fatalf("ValidatePNG() unexpected error: %v", err)
	}
}

func TestValidatePNG_SizeMismatch(t *testing.T) {
	// given: a valid PNG image with known dimensions
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	data, err := EncodePNG(img)
	if err != nil {
		t.Fatalf("EncodePNG() unexpected error: %v", err)
	}

	tests := []struct {
		name           string
		expectedWidth  int
		expectedHeight int
	}{
		{name: "wrong width", expectedWidth: 200, expectedHeight: 50},
		{name: "wrong height", expectedWidth: 100, expectedHeight: 100},
		{name: "both wrong", expectedWidth: 200, expectedHeight: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: validating with mismatched dimensions
			err := ValidatePNG(data, tt.expectedWidth, tt.expectedHeight)

			// then: error expected
			if err == nil {
				t.Fatal("ValidatePNG() expected error but got nil")
			}
		})
	}
}

func TestValidatePNG_DecodeError(t *testing.T) {
	// given: invalid PNG data
	invalidData := []byte("not a valid png")

	// when: validating
	err := ValidatePNG(invalidData, 100, 100)

	// then: error expected
	if err == nil {
		t.Fatal("ValidatePNG() expected decode error but got nil")
	}
}
