// rex/pkg/compiler/store/store.go

package store

import "github.com/redis/go-redis/v9"

type Store interface {
	MGetFacts(keys ...string) (map[string]interface{}, error)
	SetFact(key string, value interface{}) error
	GetFact(key string) (interface{}, error)
	SetAndPublishFact(key string, value interface{}) error
	Subscribe(channels ...string) *redis.PubSub
	ReceiveFacts() <-chan *redis.Message
	ScanFacts(pattern string) ([]string, error)
}
