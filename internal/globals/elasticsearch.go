package globals

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"time"

	"eck-config-operator.freepik.com/eck-config-operator/api/v1alpha1"
	"eck-config-operator.freepik.com/eck-config-operator/internal/pools"
	"github.com/elastic/go-elasticsearch/v8"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getOrCreateElasticsearchConnection retrieves or creates a connection to an Elasticsearch cluster
func GetOrCreateElasticsearchConnection(ctx context.Context, clusterKey string, resourceSelector *v1alpha1.ResourceSelector, elasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore) (*pools.ElasticsearchConnection, error) {
	logger := log.FromContext(ctx)

	// Check if connection already exists in pool
	if connection, exists := elasticsearchConnectionsPool.Get(clusterKey); exists {
		logger.Info(fmt.Sprintf("Using existing Elasticsearch connection for cluster %s", clusterKey))
		return connection, nil
	}

	logger.Info(fmt.Sprintf("Creating new Elasticsearch connection for cluster %s", clusterKey))

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
		// Use specified namespace or default to resource's namespace
		passwordSecretNamespace := resourceSelector.PasswordSecretRef.Namespace
		if passwordSecretNamespace == "" {
			passwordSecretNamespace = resourceSelector.Namespace
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
			// Use specified namespace or default to resource's namespace
			caCertSecretNamespace := resourceSelector.CACertSecretRef.Namespace
			if caCertSecretNamespace == "" {
				caCertSecretNamespace = resourceSelector.Namespace
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
		}).Namespace(resourceSelector.Namespace).Get(ctx, resourceSelector.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get ECK cluster: %w", err)
		}

		// Get the service name (ECK creates a service with name {elasticsearch-name}-es-http)
		serviceName := fmt.Sprintf("%s-es-http", resourceSelector.Name)
		endpoint = fmt.Sprintf("https://%s.%s.svc.cluster.local:9200", serviceName, resourceSelector.Namespace)

		logger.Info(fmt.Sprintf("ECK Elasticsearch endpoint: %s", endpoint))

		// Get credentials from the secret created by ECK (secret name: {elasticsearch-name}-es-elastic-user)
		secretName := fmt.Sprintf("%s-es-elastic-user", resourceSelector.Name)
		secret, err := Application.KubeRawCoreClient.CoreV1().Secrets(resourceSelector.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get Elasticsearch credentials secret: %w", err)
		}

		username = "elastic"
		password = string(secret.Data["elastic"])

		// Get the CA certificate
		caCertSecretName := fmt.Sprintf("%s-es-http-certs-public", resourceSelector.Name)
		caCertSecret, err := Application.KubeRawCoreClient.CoreV1().Secrets(resourceSelector.Namespace).Get(ctx, caCertSecretName, metav1.GetOptions{})
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

	// Verify connection
	res, err := esClient.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to verify Elasticsearch connection: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch connection verification failed: %s", res.String())
	}

	logger.Info(fmt.Sprintf("Elasticsearch connection verified for cluster %s", clusterKey))

	// Store connection in pool
	connection := &pools.ElasticsearchConnection{
		Endpoint: endpoint,
		Username: username,
		Password: password,
		CACert:   string(caCert),
		Client:   esClient,
	}

	elasticsearchConnectionsPool.Set(clusterKey, connection)

	return connection, nil
}
