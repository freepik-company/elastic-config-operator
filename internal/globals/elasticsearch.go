package globals

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"elastic-config-operator.freepik.com/elastic-config-operator/api/v1alpha1"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/pools"
	"github.com/elastic/go-elasticsearch/v8"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getOrCreateElasticsearchConnection retrieves or creates a connection to an Elasticsearch cluster
func GetOrCreateElasticsearchConnection(ctx context.Context, clusterKey string, resourceSelector *v1alpha1.ResourceSelector, crNamespace string, elasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore) (*pools.ElasticsearchConnection, error) {
	logger := log.FromContext(ctx)

	// Check if connection already exists in pool
	if connection, exists := elasticsearchConnectionsPool.Get(clusterKey); exists {
		logger.Info(fmt.Sprintf("Using existing Elasticsearch connection for cluster %s", clusterKey))
		return connection, nil
	}

	logger.Info(fmt.Sprintf("Creating new Elasticsearch connection for cluster %s", clusterKey))

	// Use resourceSelector namespace if provided, otherwise use CR namespace
	targetNamespace := resourceSelector.Namespace
	if targetNamespace == "" {
		targetNamespace = crNamespace
		logger.Info(fmt.Sprintf("ResourceSelector namespace not specified, using CR namespace: %s", targetNamespace))
	}

	var endpoint, username, password string
	var caCert []byte

	// Check if manual configuration is provided
	if resourceSelector.Endpoint != "" {
		logger.Info("Using manual Elasticsearch configuration")

		endpoint = resourceSelector.Endpoint
		logger.Info(fmt.Sprintf("Manual endpoint: %s", endpoint))

		// Get username
		if resourceSelector.Username != "" {
			username = resourceSelector.Username
		} else {
			return nil, fmt.Errorf("username is required when using manual configuration")
		}

		// Get password from secret
		if resourceSelector.PasswordSecretRef == nil {
			return nil, fmt.Errorf("passwordSecretRef is required when using manual configuration")
		}
		// Use specified namespace or default to target namespace
		passwordSecretNamespace := resourceSelector.PasswordSecretRef.Namespace
		if passwordSecretNamespace == "" {
			passwordSecretNamespace = targetNamespace
		}
		passwordSecret, err := Application.KubeRawCoreClient.CoreV1().Secrets(passwordSecretNamespace).Get(ctx, resourceSelector.PasswordSecretRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get password secret: %w", err)
		}
		password = string(passwordSecret.Data[resourceSelector.PasswordSecretRef.Key])
		if password == "" {
			return nil, fmt.Errorf("password not found in secret %s/%s key %s", passwordSecretNamespace, resourceSelector.PasswordSecretRef.Name, resourceSelector.PasswordSecretRef.Key)
		}

		// Get CA certificate from secret (optional)
		if resourceSelector.CACertSecretRef != nil {
			// Use specified namespace or default to target namespace
			caCertSecretNamespace := resourceSelector.CACertSecretRef.Namespace
			if caCertSecretNamespace == "" {
				caCertSecretNamespace = targetNamespace
			}
			caCertSecret, err := Application.KubeRawCoreClient.CoreV1().Secrets(caCertSecretNamespace).Get(ctx, resourceSelector.CACertSecretRef.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get CA certificate secret: %w", err)
			}
			caCert = caCertSecret.Data[resourceSelector.CACertSecretRef.Key]
			if len(caCert) == 0 {
				return nil, fmt.Errorf("CA certificate not found in secret %s/%s key %s", caCertSecretNamespace, resourceSelector.CACertSecretRef.Name, resourceSelector.CACertSecretRef.Key)
			}
		}
	} else {
		logger.Info("Using ECK automatic configuration")

		// Get the ECK Elasticsearch resource (we mainly need to verify it exists)
		_, err := Application.KubeRawClient.Resource(schema.GroupVersionResource{
			Group:    "elasticsearch.k8s.elastic.co",
			Version:  "v1",
			Resource: "elasticsearches",
		}).Namespace(targetNamespace).Get(ctx, resourceSelector.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get ECK cluster: %w", err)
		}

		// Get the service name (ECK creates a service with name {elasticsearch-name}-es-http)
		serviceName := fmt.Sprintf("%s-es-http", resourceSelector.Name)
		endpoint = fmt.Sprintf("https://%s.%s.svc:9200", serviceName, targetNamespace)

		logger.Info(fmt.Sprintf("ECK Elasticsearch endpoint: %s", endpoint))

		// Get credentials from the secret created by ECK (secret name: {elasticsearch-name}-es-elastic-user)
		secretName := fmt.Sprintf("%s-es-elastic-user", resourceSelector.Name)
		secret, err := Application.KubeRawCoreClient.CoreV1().Secrets(targetNamespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get Elasticsearch credentials secret: %w", err)
		}

		username = "elastic"
		password = string(secret.Data["elastic"])

		// Get the CA certificate
		caCertSecretName := fmt.Sprintf("%s-es-http-certs-public", resourceSelector.Name)
		caCertSecret, err := Application.KubeRawCoreClient.CoreV1().Secrets(targetNamespace).Get(ctx, caCertSecretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get CA certificate secret: %w", err)
		}

		caCert = caCertSecret.Data["tls.crt"]
	}

	// Create TLS config
	var tlsConfig *tls.Config
	if len(caCert) > 0 {
		// Use provided CA certificate
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig = &tls.Config{
			RootCAs: caCertPool,
		}
	} else {
		// No CA certificate provided - use system's default or skip verification
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true, // Use with caution - only for development/testing
		}
		logger.Info("No CA certificate provided, using InsecureSkipVerify (not recommended for production)")
	}

	// Create Elasticsearch client with 10 second timeout
	cfg := elasticsearch.Config{
		Addresses: []string{endpoint},
		Username:  username,
		Password:  password,
		Transport: &http.Transport{
			TLSClientConfig:       tlsConfig,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       10 * time.Second,
		},
	}

	esClient, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Verify connection and detect cluster type
	clusterType, version, err := detectClusterType(ctx, esClient, resourceSelector.ClusterType)
	if err != nil {
		return nil, fmt.Errorf("failed to detect cluster type: %w", err)
	}

	logger.Info(fmt.Sprintf("Detected cluster type: %s, version: %s", clusterType, version))

	// Store connection in pool
	connection := &pools.ElasticsearchConnection{
		Endpoint:    endpoint,
		Username:    username,
		Password:    password,
		CACert:      string(caCert),
		Client:      esClient,
		ClusterType: clusterType,
		Version:     version,
	}

	elasticsearchConnectionsPool.Set(clusterKey, connection)

	return connection, nil
}

// detectClusterType detects the type of cluster (Elasticsearch or OpenSearch) and its version
// If clusterTypeOverride is provided, it will use that instead of auto-detection
func detectClusterType(ctx context.Context, client *elasticsearch.Client, clusterTypeOverride string) (string, string, error) {
	logger := log.FromContext(ctx)

	// If cluster type is explicitly provided, use it
	if clusterTypeOverride != "" {
		logger.Info(fmt.Sprintf("Using manually configured cluster type: %s", clusterTypeOverride))
		// Still need to get the version
		res, err := client.Info(client.Info.WithContext(ctx))
		if err != nil {
			return "", "", fmt.Errorf("failed to get cluster info: %w", err)
		}
		defer res.Body.Close()

		if res.IsError() {
			return "", "", fmt.Errorf("cluster info request failed: %s", res.String())
		}

		var info struct {
			Version struct {
				Number string `json:"number"`
			} `json:"version"`
		}

		bodyBytes, err := io.ReadAll(res.Body)
		if err != nil {
			return "", "", fmt.Errorf("failed to read response body: %w", err)
		}

		if err := json.Unmarshal(bodyBytes, &info); err != nil {
			return "", "", fmt.Errorf("failed to parse cluster info: %w", err)
		}

		return clusterTypeOverride, info.Version.Number, nil
	}

	// Auto-detect cluster type
	res, err := client.Info(client.Info.WithContext(ctx))
	if err != nil {
		return "", "", fmt.Errorf("failed to get cluster info: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return "", "", fmt.Errorf("cluster info request failed: %s", res.String())
	}

	var info struct {
		Version struct {
			Distribution string `json:"distribution"` // OpenSearch includes this field
			Number       string `json:"number"`
		} `json:"version"`
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &info); err != nil {
		return "", "", fmt.Errorf("failed to parse cluster info: %w", err)
	}

	// OpenSearch explicitly includes "distribution": "opensearch"
	// Elasticsearch may not include this field or has "elasticsearch"
	clusterType := "elasticsearch"
	if info.Version.Distribution == "opensearch" {
		clusterType = "opensearch"
	}

	logger.Info(fmt.Sprintf("Auto-detected cluster type: %s (version: %s)", clusterType, info.Version.Number))

	return clusterType, info.Version.Number, nil
}
