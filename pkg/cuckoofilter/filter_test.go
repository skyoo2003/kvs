package cuckoofilter

import (
	"errors"
	"testing"
)

func TestNewFilter(t *testing.T) {
	filter := NewFilter()

	if filter.tableSize != DefaultTables {
		t.Fatalf("NewFilter() tableSize = %v, want %v", filter.tableSize, DefaultTables)
	}
	if len(filter.tables) != DefaultTables {
		t.Fatalf("NewFilter() len(tables) = %v, want %v", len(filter.tables), DefaultTables)
	}
	if filter.size != 0 {
		t.Fatalf("NewFilter() size = %v, want %v", filter.size, 0)
	}
	if filter.tables[0] == nil {
		t.Fatal("NewFilter() first table is nil")
	}
	if filter.tables[len(filter.tables)-1] == nil {
		t.Fatal("NewFilter() last table is nil")
	}
}

func TestNewFilterWithTableSize(t *testing.T) {
	filter := NewFilterWithTableSize(8)

	if filter.tableSize != 8 {
		t.Fatalf("NewFilterWithTableSize() tableSize = %v, want %v", filter.tableSize, 8)
	}
	if len(filter.tables) != 8 {
		t.Fatalf("NewFilterWithTableSize() len(tables) = %v, want %v", len(filter.tables), 8)
	}
	if filter.Size() != 0 {
		t.Fatalf("NewFilterWithTableSize() size = %v, want %v", filter.Size(), 0)
	}
}

func TestFilter_Size(t *testing.T) {
	f := &Filter{size: 3}

	if got := f.Size(); got != 3 {
		t.Fatalf("Size() = %v, want %v", got, 3)
	}
}

func TestFilter_Insert(t *testing.T) {
	f := NewFilterWithTableSize(8)
	target := []byte("insert-me")

	if err := f.Insert(target); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if !f.Lookup(target) {
		t.Fatal("Lookup() = false, want true")
	}
	if f.Size() != 1 {
		t.Fatalf("Size() after Insert() = %v, want %v", f.Size(), 1)
	}
}

func TestFilter_Lookup(t *testing.T) {
	type fields struct {
		tables    []*Table
		tableSize int
		size      int
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{
				tables:    tt.fields.tables,
				tableSize: tt.fields.tableSize,
				size:      tt.fields.size,
			}
			if got := f.Lookup(tt.args.data); got != tt.want {
				t.Errorf("Filter.Lookup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Delete(t *testing.T) {
	target := []byte("delete-me")
	f := NewFilterWithTableSize(8)

	if err := f.Insert(target); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if err := f.Delete(target); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if f.Size() != 0 {
		t.Fatalf("Size() after Delete() = %v, want %v", f.Size(), 0)
	}

	if err := f.Delete(target); !errors.Is(err, ErrNoData) {
		t.Fatalf("Delete() missing data error = %v, want %v", err, ErrNoData)
	}
}

func TestFilterInsertReturnsReallocationErrorWhenSmallFilterIsFull(t *testing.T) {
	f := &Filter{
		tables: []*Table{
			{buckets: []fingerprint{1}, bucketSize: 1, size: 1},
			{buckets: []fingerprint{2}, bucketSize: 1, size: 1},
		},
		tableSize: 2,
		size:      2,
	}

	if err := f.Insert([]byte("overflow")); !errors.Is(err, ErrReallocationFails) {
		t.Fatalf("Insert() error = %v, want %v", err, ErrReallocationFails)
	}
}

func TestFilter_index(t *testing.T) {
	type fields struct {
		tables    []*Table
		tableSize int
		size      int
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{
				tables:    tt.fields.tables,
				tableSize: tt.fields.tableSize,
				size:      tt.fields.size,
			}
			if got := f.index(tt.args.data); got != tt.want {
				t.Errorf("Filter.index() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_altIndex(t *testing.T) {
	type fields struct {
		tables    []*Table
		tableSize int
		size      int
	}
	type args struct {
		idx int
		fp  fingerprint
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{
				tables:    tt.fields.tables,
				tableSize: tt.fields.tableSize,
				size:      tt.fields.size,
			}
			if got := f.altIndex(tt.args.idx, tt.args.fp); got != tt.want {
				t.Errorf("Filter.altIndex() = %v, want %v", got, tt.want)
			}
		})
	}
}
