package cuckoofilter

import "testing"

func Test_obtainsFingerprint(t *testing.T) {
	type args struct {
		data []byte
	}
	tests := []struct {
		name string
		args args
		want fingerprint
	}{
		{"empty data", args{[]byte{}}, 29806},
		{"arbitrary data", args{[]byte("Hello, World!")}, 52576},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := obtainsFingerprint(tt.args.data); got != tt.want {
				t.Errorf("obtainsFingerprint() = %v, want %v", got, tt.want)
			}
		})
	}
}
