/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pools

import (
	"sync"

	"github.com/elastic/go-elasticsearch/v8"
)

// ElasticsearchConnection holds the connection details and client for an Elasticsearch cluster
type ElasticsearchConnection struct {
	Endpoint    string
	Username    string
	Password    string
	CACert      string
	Client      *elasticsearch.Client
	ClusterType string // "elasticsearch" or "opensearch"
	Version     string // cluster version (e.g., "8.11.0", "2.11.0")
}

// ElasticsearchConnectionsStore stores Elasticsearch connections by namespace_name
type ElasticsearchConnectionsStore struct {
	mu    sync.RWMutex
	Store map[string]*ElasticsearchConnection
}

func (c *ElasticsearchConnectionsStore) Set(key string, connection *ElasticsearchConnection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Store[key] = connection
}

func (c *ElasticsearchConnectionsStore) Get(key string) (*ElasticsearchConnection, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	connection, exists := c.Store[key]
	return connection, exists
}

func (c *ElasticsearchConnectionsStore) GetAll() map[string]*ElasticsearchConnection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Store
}

func (c *ElasticsearchConnectionsStore) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Store, key)
}
