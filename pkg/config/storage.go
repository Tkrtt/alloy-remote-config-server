package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	redis "github.com/go-redis/redis/v8"
)

type Storage struct {
	UseRedis     bool
	MemoryStore  map[string]string
	MemoryTemplateMap map[string]string  // Maps config ID to template name
	RedisStore   *redis.Client
	Organization string
	TTL          time.Duration
}

func InitStorage() (*Storage, error) {
	orgName := os.Getenv("ORG_NAME")
	useRedis := os.Getenv("USE_REDIS") == "true"
	if useRedis {
		redisOpt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
		if err != nil {
			return nil, err
		}
		redisOpt.DB = 0
		redisOpt.IdleTimeout = time.Second * 60
		redisOpt.IdleCheckFrequency = time.Second * 5
		redisClient := redis.NewClient(redisOpt)
		ttlNum := 259200
		ttlEnv := os.Getenv("REDIS_TTL")
		if len(ttlEnv) > 0 {
			ttlNum, _ = strconv.Atoi(ttlEnv)
		}
		ttlDuration := time.Duration(ttlNum) * time.Second
		return &Storage{RedisStore: redisClient, UseRedis: true, MemoryTemplateMap: make(map[string]string), Organization: orgName, TTL: ttlDuration}, nil
	} else {
		return &Storage{MemoryStore: make(map[string]string), MemoryTemplateMap: make(map[string]string), UseRedis: false, Organization: orgName}, nil
	}
}

func (s *Storage) SetWithTemplate(id string, content string, templateName string) error {
	key := fmt.Sprintf("{%s}:%s", s.Organization, id)
	if s.UseRedis {
		// For Redis, store the config content and separately track the template relationship
		ctx := context.Background()
		err := s.RedisStore.Set(ctx, key, content, 0).Err()
		if err != nil {
			return err
		}
		_, err = s.RedisStore.Expire(ctx, key, s.TTL).Result()
		if err != nil {
			return err
		}
		// Store the template information (this would require additional Redis keys or approach)
		templateKey := fmt.Sprintf("{%s}:template:%s", s.Organization, id)
		err = s.RedisStore.Set(ctx, templateKey, templateName, s.TTL).Err()
		if err != nil {
			return err
		}
	} else {
		s.MemoryStore[key] = content
		s.MemoryTemplateMap[key] = templateName
	}
	return nil
}

func (s *Storage) Set(id string, content string) error {
	// For backward compatibility, call SetWithTemplate with a default template name
	return s.SetWithTemplate(id, content, "unknown")
}

// GetTemplate returns the template name that was used to generate the config with the given ID
func (s *Storage) GetTemplate(id string) (string, error) {
	if s.UseRedis {
		ctx := context.Background()
		templateKey := fmt.Sprintf("{%s}:template:%s", s.Organization, id)
		templateName, err := s.RedisStore.Get(ctx, templateKey).Result()
		if err != nil {
			return "", fmt.Errorf("Template information not found for config: %s", id)
		}
		return templateName, nil
	} else {
		templateKey := fmt.Sprintf("{%s}:%s", s.Organization, id)
		templateName, exists := s.MemoryTemplateMap[templateKey]
		if !exists {
			return "", fmt.Errorf("Template information not found for config: %s", id)
		}
		return templateName, nil
	}
}

// RemoveByTemplate removes all configs that were generated from the specified template
func (s *Storage) RemoveByTemplate(templateName string) error {
	if s.UseRedis {
		// For Redis, we need to scan all keys to find configs generated from the template
		ctx := context.Background()
		orgPrefix := fmt.Sprintf("{%s}:", s.Organization)
		matchPattern := fmt.Sprintf("%s*", orgPrefix)

		// Scan for all config keys first
		var cursor uint64
		for {
			var configKeys []string
			var err error
			configKeys, cursor, err = s.RedisStore.Scan(ctx, cursor, matchPattern, 100).Result()
			if err != nil {
				return err
			}

			// Check each config key to see if it was generated from the template
			for _, configKey := range configKeys {
				// Skip template tracking keys and other metadata keys
				if strings.Contains(configKey, ":template:") {
					continue
				}

				// Get the template for this config
				templateKey := fmt.Sprintf("%s:template:%s", orgPrefix, strings.TrimPrefix(configKey, orgPrefix))
				storedTemplateName, err := s.RedisStore.Get(ctx, templateKey).Result()
				if err != nil || storedTemplateName != templateName {
					continue
				}

				// Remove both the config and its template tracking
				err = s.RedisStore.Del(ctx, configKey).Err()
				if err != nil {
					log.Printf("Error deleting config key %s: %v", configKey, err)
				}
				err = s.RedisStore.Del(ctx, templateKey).Err()
				if err != nil {
					log.Printf("Error deleting template key %s: %v", templateKey, err)
				}
			}

			if cursor == 0 {
				break
			}
		}
	} else {
		// For memory store, we can directly iterate through the template map
		keysToRemove := []string{}
		for configKey, storedTemplateName := range s.MemoryTemplateMap {
			if storedTemplateName == templateName {
				delete(s.MemoryStore, configKey)
				keysToRemove = append(keysToRemove, configKey)
			}
		}
		for _, key := range keysToRemove {
			delete(s.MemoryTemplateMap, key)
		}
	}
	return nil
}

func (s *Storage) Get(id string) (string, error) {
	key := fmt.Sprintf("{%s}:%s", s.Organization, id)
	if s.UseRedis {
		ctx := context.Background()
		value, err := s.RedisStore.Get(ctx, key).Result()
		if err != nil {
			return "", fmt.Errorf("Key (id) does not exist: %s", id)
		}
		return value, nil
	} else {
		value, exists := s.MemoryStore[key]
		if !exists {
			return "", fmt.Errorf("Key (id) does not exist: %s", id)
		}
		return value, nil
	}
}

func (s *Storage) GetAll() ([]string, error) {
	result := make([]string, 0)
	orgPrefix := fmt.Sprintf("{%s}:", s.Organization)
	if s.UseRedis {
		ctx := context.Background()
		scanCount := 100
		match := fmt.Sprintf("%s*", orgPrefix)
		var cursor uint64
		for {
			var ks []string
			var err error
			ks, cursor, err = s.RedisStore.Scan(ctx, cursor, match, int64(scanCount)).Result()
			if err != nil {
				return nil, err
			}
			for _, k := range ks {
				result = append(result, strings.TrimPrefix(k, orgPrefix))
			}
			if cursor == 0 {
				break
			}
		}
	} else {
		for k, _ := range s.MemoryStore {
			result = append(result, strings.TrimPrefix(k, orgPrefix))
		}
	}
	return result, nil
}
