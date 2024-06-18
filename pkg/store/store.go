// rex/pkg/compiler/store/store.go

package store

type Store interface {
	SetFact(key string, value interface{}) error
	GetFact(key string) (interface{}, error)
	MGetFacts(keys ...string) (map[string]interface{}, error)
}
