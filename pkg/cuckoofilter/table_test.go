package cuckoofilter

import (
	"reflect"
	"testing"
)

func TestNewTable(t *testing.T) {
	tests := []struct {
		name string
		want *Table
	}{
		{"new table", &Table{
			buckets:    make([]fingerprint, DefaultBuckets),
			bucketSize: DefaultBuckets,
			size:       0,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewTable(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewTable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTableWithBucketSize(t *testing.T) {
	type args struct {
		bucketSize int
	}
	tests := []struct {
		name string
		args args
		want *Table
	}{
		{"new table with bucket size", args{8}, &Table{
			buckets:    make([]fingerprint, 8),
			bucketSize: 8,
			size:       0,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewTableWithBucketSize(tt.args.bucketSize); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewTableWithBucketSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTable_Insert(t *testing.T) {
	type fields struct {
		buckets    []fingerprint
		bucketSize int
		size       int
	}
	type args struct {
		fp fingerprint
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			"table is empty",
			fields{make([]fingerprint, 1), 1, 0},
			args{obtainsFingerprint([]byte{})},
			false,
		},
		{
			"table is full",
			fields{[]fingerprint{obtainsFingerprint([]byte{})}, 1, 1},
			args{obtainsFingerprint([]byte{})},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Table{
				buckets:    tt.fields.buckets,
				bucketSize: tt.fields.bucketSize,
				size:       tt.fields.size,
			}
			if err := tr.Insert(tt.args.fp); (err != nil) != tt.wantErr {
				t.Errorf("Table.Insert() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTable_Delete(t *testing.T) {
	type fields struct {
		buckets    []fingerprint
		bucketSize int
		size       int
	}
	type args struct {
		fp fingerprint
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			"table has no entry",
			fields{make([]fingerprint, 1), 1, 0},
			args{obtainsFingerprint([]byte{})},
			true,
		},
		{
			"table has an entry",
			fields{[]fingerprint{obtainsFingerprint([]byte{})}, 1, 1},
			args{obtainsFingerprint([]byte{})},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Table{
				buckets:    tt.fields.buckets,
				bucketSize: tt.fields.bucketSize,
				size:       tt.fields.size,
			}
			if err := tr.Delete(tt.args.fp); (err != nil) != tt.wantErr {
				t.Errorf("Table.Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTable_InsertUsesFreedBucket(t *testing.T) {
	first := obtainsFingerprint([]byte("first"))
	second := obtainsFingerprint([]byte("second"))
	third := obtainsFingerprint([]byte("third"))

	table := &Table{
		buckets:    []fingerprint{first, second},
		bucketSize: 2,
		size:       2,
	}

	if err := table.Delete(first); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if err := table.Insert(third); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if table.Index(second) == -1 {
		t.Fatal("Insert() overwrote an existing fingerprint")
	}
	if table.Index(third) == -1 {
		t.Fatal("Insert() did not place the new fingerprint in the freed bucket")
	}
}

func TestTable_Index(t *testing.T) {
	type fields struct {
		buckets    []fingerprint
		bucketSize int
		size       int
	}
	type args struct {
		fp fingerprint
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		{
			"table has no entry",
			fields{make([]fingerprint, 1), 1, 0},
			args{obtainsFingerprint([]byte{})},
			-1,
		},
		{
			"table has an entry",
			fields{[]fingerprint{obtainsFingerprint([]byte{})}, 1, 1},
			args{obtainsFingerprint([]byte{})},
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Table{
				buckets:    tt.fields.buckets,
				bucketSize: tt.fields.bucketSize,
				size:       tt.fields.size,
			}
			if got := tr.Index(tt.args.fp); got != tt.want {
				t.Errorf("Table.Index() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTable_Reset(t *testing.T) {
	type fields struct {
		buckets    []fingerprint
		bucketSize int
		size       int
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			"table has an entry",
			fields{[]fingerprint{obtainsFingerprint([]byte{})}, 1, 1},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Table{
				buckets:    tt.fields.buckets,
				bucketSize: tt.fields.bucketSize,
				size:       tt.fields.size,
			}
			if err := tr.Reset(); (err != nil) != tt.wantErr {
				t.Errorf("Table.Reset() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
