package client_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/zesty-co/terraform-provider-zesty/internal/client"
	"github.com/zesty-co/terraform-provider-zesty/internal/models"
)

const AUTH_HEADER string = "X-Api-Key"

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		host        *string
		token       string
		expectedURL string
		expectError bool
	}{
		{
			name:        "nil host uses default",
			host:        nil,
			token:       "testtoken",
			expectedURL: models.DefaultHostURL,
			expectError: false,
		},
		{
			name:        "provided host is used",
			host:        func() *string { s := "http://customhost:1234"; return &s }(),
			token:       "testtoken2",
			expectedURL: "http://customhost:1234",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := client.NewClient(tt.host, tt.token)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, c)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, c)
				assert.Equal(t, tt.expectedURL, c.HostURL)
				assert.Equal(t, tt.token, c.Token)
				assert.NotNil(t, c.HTTPClient)
				assert.Equal(t, 180*time.Second, c.HTTPClient.Timeout)
			}
		})
	}
}

func TestClient_DoRequest(t *testing.T) {
	type testCase struct {
		name             string
		token            string
		serverHandler    http.HandlerFunc
		expectedBody     []byte
		expectedErrorMsg string
		method           string
		path             string
		requestBody      io.Reader
	}

	tests := []testCase{
		{
			name:  "successful GET request",
			token: "goodtoken",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				authHeader := r.Header.Get(AUTH_HEADER)
				assert.Equal(t, "goodtoken", authHeader)
				assert.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"message":"success"}`))
			},
			method:           http.MethodGet,
			path:             "/test",
			expectedBody:     []byte(`{"message":"success"}`),
			expectedErrorMsg: "",
		},
		{
			name:  "successful POST request with status created",
			token: "goodtoken",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				authHeader := r.Header.Get(AUTH_HEADER)
				assert.Equal(t, "goodtoken", authHeader)
				assert.Equal(t, http.MethodPost, r.Method)
				bodyBytes, _ := io.ReadAll(r.Body)
				assert.JSONEq(t, `{"key":"value"}`, string(bodyBytes))
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"123"}`))
			},
			method:           http.MethodPost,
			path:             "/create",
			requestBody:      bytes.NewReader([]byte(`{"key":"value"}`)),
			expectedBody:     []byte(`{"id":"123"}`),
			expectedErrorMsg: "",
		},
		{
			name:  "server error status forbidden",
			token: "anytoken",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
			},
			method:           http.MethodGet,
			path:             "/forbidden",
			expectedBody:     nil,
			expectedErrorMsg: "status: 403, body: {\"error\":\"forbidden\"}",
		},
		{
			name:  "server error status not found",
			token: "anytoken",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not found"}`))
			},
			method:           http.MethodDelete,
			path:             "/nonexistent",
			expectedBody:     nil,
			expectedErrorMsg: "status: 404, body: {\"error\":\"not found\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tt.path {
					tt.serverHandler(w, r)
				} else {
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			c, _ := client.NewClient(&server.URL, tt.token)
			req, err := http.NewRequest(tt.method, server.URL+tt.path, tt.requestBody)
			assert.NoError(t, err)

			body, err := c.DoRequest(req)

			if tt.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
				assert.Nil(t, body)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedBody, body)
			}
		})
	}

	t.Run("http client do error - connection refused", func(t *testing.T) {
		nonExistentURL := "http://localhost:12345"
		c, _ := client.NewClient(&nonExistentURL, "test")
		c.HTTPClient = &http.Client{Timeout: 100 * time.Millisecond}

		req, _ := http.NewRequest("GET", nonExistentURL+"/test", nil)
		_, err := c.DoRequest(req)
		assert.Error(t, err)
	})
}

func TestClient_Validate(t *testing.T) {
	type testCase struct {
		name             string
		token            string
		serverHandler    http.HandlerFunc
		expectedAccount  *models.Account
		expectedErrorMsg string
	}

	tests := []testCase{
		{
			name:  "successful validate",
			token: "get-token",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/validate", r.URL.Path)
				assert.Equal(t, "get-token", r.Header.Get(AUTH_HEADER))

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"message":"success"}`))
			},
			expectedErrorMsg: "",
		},
		{
			name:  "failed validate",
			token: "get-err-token",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "get-err-token", r.Header.Get(AUTH_HEADER))
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Forbidden"))
			},
			expectedAccount:  nil,
			expectedErrorMsg: "status: 403, body: Forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			c, _ := client.NewClient(&server.URL, tt.token)
			err := c.Validate()

			if tt.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClient_CreateAccount(t *testing.T) {
	type testCase struct {
		name             string
		payload          models.Payload
		serverHandler    http.HandlerFunc
		expectedAccount  *models.Account
		expectedErrorMsg string
		token            string
	}

	sampleExpectedAccount := &models.Account{
		AccountID:     "acc123",
		CloudProvider: models.AWS,
		AdditionalData: map[string]any{
			"roleARN":    "arn:aws:iam::123456789012:role/MyRole",
			"externalID": "someExternalID",
		},
		Products: map[models.Product]models.ProductDetails{
			models.Kompass: {Active: true},
		},
	}
	sampleExpectedAccountBytes, _ := json.Marshal(sampleExpectedAccount)

	tests := []testCase{
		{
			name:  "successful creation",
			token: "create-token",
			payload: models.Payload{
				AccountID:     "acc123",
				CloudProvider: models.AWS,
				RoleARN:       "arn:aws:iam::123456789012:role/MyRole",
				ExternalID:    "someExternalID",
				Products: map[models.Product]models.ProductDetails{
					models.Kompass: {Active: true},
					models.CM:      {Active: false},
				},
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/account", r.URL.Path)
				assert.Equal(t, "create-token", r.Header.Get(AUTH_HEADER))

				var p models.Payload
				err := json.NewDecoder(r.Body).Decode(&p)
				if !assert.NoError(t, err) {
					http.Error(w, "bad request body", http.StatusBadRequest)
					return
				}
				assert.Equal(t, "acc123", p.AccountID)
				assert.Equal(t, models.AWS, p.CloudProvider)
				assert.Equal(t, "arn:aws:iam::123456789012:role/MyRole", p.RoleARN)
				assert.Equal(t, "someExternalID", p.ExternalID)
				assert.True(t, p.Products[models.Kompass].Active)
				assert.False(t, p.Products[models.CM].Active)

				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write(sampleExpectedAccountBytes)
			},
			expectedAccount:  sampleExpectedAccount,
			expectedErrorMsg: "",
		},
		{
			name:  "server returns error",
			token: "err-token",
			payload: models.Payload{
				AccountID: "acc456",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "err-token", r.Header.Get(AUTH_HEADER))
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			expectedAccount:  nil,
			expectedErrorMsg: "status: 500, body: internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			c, _ := client.NewClient(&server.URL, tt.token)
			account, err := c.CreateAccount(tt.payload)

			if tt.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
				assert.Nil(t, account)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedAccount, account)
			}
		})
	}
}

func TestClient_DeleteAccount(t *testing.T) {
	type testCase struct {
		name             string
		organizationID   int
		accountID        string
		serverHandler    http.HandlerFunc
		expectedErrorMsg string
		token            string
	}

	tests := []testCase{
		{
			name:           "successful deletion",
			token:          "delete-token",
			organizationID: 1,
			accountID:      "acc123",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "DELETE", r.Method)
				assert.Equal(t, "/account", r.URL.Path)
				assert.Equal(t, "delete-token", r.Header.Get(AUTH_HEADER))

				var p models.Payload
				err := json.NewDecoder(r.Body).Decode(&p)
				if !assert.NoError(t, err) {
					http.Error(w, "bad request body for delete", http.StatusBadRequest)
					return
				}
				assert.Equal(t, "acc123", p.AccountID)

				w.WriteHeader(http.StatusOK)
			},
			expectedErrorMsg: "",
		},
		{
			name:           "server returns error",
			token:          "del-err-token",
			organizationID: 2,
			accountID:      "acc456",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "del-err-token", r.Header.Get(AUTH_HEADER))
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			expectedErrorMsg: "status: 404, body: not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			c, _ := client.NewClient(&server.URL, tt.token)

			payload := models.Payload{AccountID: tt.accountID}
			err := c.DeleteAccount(payload)

			if tt.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClient_GetAccount(t *testing.T) {
	type testCase struct {
		name             string
		accountID        string
		serverHandler    http.HandlerFunc
		expectedAccount  *models.Account
		expectedErrorMsg string
		token            string
	}

	sampleGetAccount := &models.Account{
		AccountID:     "acc123",
		CloudProvider: models.GCP,
		AdditionalData: map[string]any{
			"roleARN":    "projects/my-gcp-project/serviceAccounts/my-sa@my-gcp-project.iam.gserviceaccount.com",
			"externalID": "gcpExternalID",
		},
		Products: map[models.Product]models.ProductDetails{
			models.ZestyDisk: {Active: true},
		},
	}
	sampleGetAccountBytes, _ := json.Marshal(sampleGetAccount)

	tests := []testCase{
		{
			name:      "successful get",
			token:     "get-token",
			accountID: "acc123",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/account", r.URL.Path)
				assert.Equal(t, "get-token", r.Header.Get(AUTH_HEADER))
				assert.Equal(t, "acc123", r.URL.Query().Get("accountID"))

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(sampleGetAccountBytes)
			},
			expectedAccount:  sampleGetAccount,
			expectedErrorMsg: "",
		},
		{
			name:      "server returns error",
			token:     "get-err-token",
			accountID: "acc456",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "get-err-token", r.Header.Get(AUTH_HEADER))
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			expectedAccount:  nil,
			expectedErrorMsg: "status: 404, body: not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			c, _ := client.NewClient(&server.URL, tt.token)
			account, err := c.GetAccount(tt.accountID)

			if tt.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
				assert.Nil(t, account)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedAccount, account)
			}
		})
	}
}

func TestClient_GetAccount_RetriesTransientErrors(t *testing.T) {
	sampleAccount := &models.Account{AccountID: "acc123", CloudProvider: models.AWS}
	sampleAccountBytes, _ := json.Marshal(sampleAccount)

	t.Run("recovers after gateway timeouts", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls <= 2 {
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte(`{"message": "Network error communicating with endpoint"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(sampleAccountBytes)
		}))
		defer server.Close()

		c, _ := client.NewClient(&server.URL, "retry-token")
		c.RetryBaseDelay = time.Millisecond

		account, err := c.GetAccount("acc123")
		assert.NoError(t, err)
		assert.Equal(t, sampleAccount, account)
		assert.Equal(t, 3, calls)
	})

	t.Run("gives up after configured attempts", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte("gateway timeout"))
		}))
		defer server.Close()

		c, _ := client.NewClient(&server.URL, "retry-token")
		c.RetryBaseDelay = time.Millisecond

		account, err := c.GetAccount("acc123")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status: 504")
		assert.Nil(t, account)
		assert.Equal(t, 4, calls)
	})

	t.Run("does not retry non-transient statuses", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		}))
		defer server.Close()

		c, _ := client.NewClient(&server.URL, "retry-token")
		c.RetryBaseDelay = time.Millisecond

		account, err := c.GetAccount("acc123")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status: 404")
		assert.Nil(t, account)
		assert.Equal(t, 1, calls)
	})

	t.Run("retries connection errors", func(t *testing.T) {
		nonExistentURL := "http://localhost:12345"
		c, _ := client.NewClient(&nonExistentURL, "retry-token")
		c.HTTPClient = &http.Client{Timeout: 100 * time.Millisecond}
		c.RetryBaseDelay = time.Millisecond

		account, err := c.GetAccount("acc123")
		assert.Error(t, err)
		assert.Nil(t, account)
	})
}

func TestClient_CreateAccount_DoesNotRetry(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("gateway timeout"))
	}))
	defer server.Close()

	c, _ := client.NewClient(&server.URL, "create-token")
	c.RetryBaseDelay = time.Millisecond

	account, err := c.CreateAccount(models.Payload{AccountID: "acc123"})
	assert.Error(t, err)
	assert.Nil(t, account)
	assert.Equal(t, 1, calls)
}

func TestClient_UpdateAccount(t *testing.T) {
	type testCase struct {
		name             string
		payload          models.Payload
		serverHandler    http.HandlerFunc
		expectedAccount  *models.Account
		expectedErrorMsg string
		token            string
	}

	sampleUpdatedAccount := &models.Account{
		AccountID:     "acc123",
		CloudProvider: models.Azure,
		AdditionalData: map[string]any{
			"roleARN":    "/subscriptions/subid/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/myidentity",
			"externalID": "azureExternalIDUpdated",
		},
		Products: map[models.Product]models.ProductDetails{
			models.Kompass: {Active: false},
			models.CM:      {Active: true},
		},
	}
	sampleUpdatedAccountBytes, _ := json.Marshal(sampleUpdatedAccount)

	tests := []testCase{
		{
			name:  "successful update",
			token: "update-token",
			payload: models.Payload{
				AccountID:     "acc123",
				CloudProvider: models.Azure,
				RoleARN:       "/subscriptions/subid/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/myidentity",
				ExternalID:    "azureExternalIDUpdated",
				Products: map[models.Product]models.ProductDetails{
					models.Kompass: {Active: false},
					models.CM:      {Active: true},
				},
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "PUT", r.Method)
				assert.Equal(t, "/account", r.URL.Path)
				assert.Equal(t, "update-token", r.Header.Get(AUTH_HEADER))

				var p models.Payload
				err := json.NewDecoder(r.Body).Decode(&p)
				if !assert.NoError(t, err) {
					http.Error(w, "bad request body", http.StatusBadRequest)
					return
				}
				assert.Equal(t, "acc123", p.AccountID)
				assert.Equal(t, models.Azure, p.CloudProvider)
				assert.Equal(t, "/subscriptions/subid/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/myidentity", p.RoleARN)
				assert.False(t, p.Products[models.Kompass].Active)
				assert.True(t, p.Products[models.CM].Active)

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(sampleUpdatedAccountBytes)
			},
			expectedAccount:  sampleUpdatedAccount,
			expectedErrorMsg: "",
		},
		{
			name:  "server returns error",
			token: "update-err-token",
			payload: models.Payload{
				AccountID: "acc456",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "update-err-token", r.Header.Get(AUTH_HEADER))
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad request"))
			},
			expectedAccount:  nil,
			expectedErrorMsg: "status: 400, body: bad request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			c, _ := client.NewClient(&server.URL, tt.token)
			account, err := c.UpdateAccount(tt.payload)

			if tt.expectedErrorMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorMsg)
				assert.Nil(t, account)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedAccount, account)
			}
		})
	}
}
