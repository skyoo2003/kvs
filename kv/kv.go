package kv

type KVStore struct {
}

func (kv *KVStore) Get(key string) (interface{}, error) {
	return nil, nil
}

func (kv *KVStore) Put(key string, value interface{}) error {
	return nil
}

func (kv *KVStore) Delete(key string) error {
	return nil
}
