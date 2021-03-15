package cuckoofilter

import (
	"reflect"
	"testing"
)

func TestNewFilter(t *testing.T) {
	tables := make([]*Table, DefaultTables)
	for i := 0; i < DefaultTables; i++ {
		tables[i] = NewTable()
	}
	tests := []struct {
		name string
		want *Filter
	}{
		{"empty tables", &Filter{
			tables:    tables,
			tableSize: DefaultTables,
			size:      0,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewFilter(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewFilterWithTableSize(t *testing.T) {
	type args struct {
		tableSize int
	}
	tests := []struct {
		name string
		args args
		want *Filter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewFilterWithTableSize(tt.args.tableSize); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewFilterWithTableSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Size(t *testing.T) {
	type fields struct {
		tables    []*Table
		tableSize int
		size      int
	}
	tests := []struct {
		name   string
		fields fields
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
			if got := f.Size(); got != tt.want {
				t.Errorf("Filter.Size() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Insert(t *testing.T) {
	type fields struct {
		tables    []*Table
		tableSize int
		size      int
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
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
			if err := f.Insert(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("Filter.Insert() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
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
	type fields struct {
		tables    []*Table
		tableSize int
		size      int
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
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
			if err := f.Delete(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("Filter.Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
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
