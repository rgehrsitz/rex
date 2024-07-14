// rex/pkg/compiler/store/store.go

package store

import "github.com/redis/go-redis/v9"

type Store interface {
	SetFact(key string, value interface{}) error
	SetAndPublishFact(key string, value interface{}) error
	GetFact(key string) (interface{}, error)
	MGetFacts(keys ...string) (map[string]interface{}, error)
	ReceiveFacts() <-chan *redis.Message // Add this line
}
