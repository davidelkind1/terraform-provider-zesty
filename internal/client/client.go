package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zesty-co/terraform-provider-zesty/internal/models"
)

const (
	defaultRetryAttempts  = 4
	defaultRetryBaseDelay = 2 * time.Second
)

type Client struct {
	HostURL        string
	HTTPClient     *http.Client
	Token          string
	RetryAttempts  int
	RetryBaseDelay time.Duration
}

func NewClient(host *string, token string) (*Client, error) {
	c := Client{
		HTTPClient:     &http.Client{Timeout: 180 * time.Second},
		HostURL:        models.DefaultHostURL,
		RetryAttempts:  defaultRetryAttempts,
		RetryBaseDelay: defaultRetryBaseDelay,
	}

	if host != nil {
		c.HostURL = *host
	}

	c.Token = token

	return &c, nil
}

func (c *Client) Validate() error {
	url := fmt.Sprintf("%s/validate", c.HostURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	_, err = c.DoRequestWithRetry(req)
	return err
}

func (c *Client) DoRequest(req *http.Request) ([]byte, error) {
	body, _, err := c.do(req)
	return body, err
}

// DoRequestWithRetry sends a request, retrying transport errors and transient
// upstream statuses (429/502/503/504) with exponential backoff. The onboarding
// API sits behind an API gateway whose integration can intermittently time out,
// so reads during Terraform refresh must tolerate one-off gateway failures.
// Only use it for requests that are safe to repeat.
func (c *Client) DoRequestWithRetry(req *http.Request) ([]byte, error) {
	attempts := c.RetryAttempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(c.RetryBaseDelay << (attempt - 1))
		}

		body, status, err := c.do(req.Clone(req.Context()))
		if err == nil {
			return body, nil
		}
		lastErr = err

		if status != 0 && !retryableStatus(status) {
			return nil, err
		}
	}

	return nil, lastErr
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (c *Client) do(req *http.Request) ([]byte, int, error) {
	req.Header.Set("x-api-key", c.Token)

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = res.Body.Close()
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, err
	}

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, res.StatusCode, fmt.Errorf("status: %d, body: %s", res.StatusCode, body)
	}

	return body, res.StatusCode, nil
}

func (c *Client) CreateAccount(payload models.Payload) (*models.Account, error) {
	rb, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/account", c.HostURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(rb))
	if err != nil {
		return nil, err
	}

	body, err := c.DoRequest(req)
	if err != nil {
		return nil, err
	}

	account := models.Account{}
	err = json.Unmarshal(body, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

func (c *Client) DeleteAccount(payload models.Payload) error {
	rb, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/account", c.HostURL)
	req, err := http.NewRequest("DELETE", url, bytes.NewReader(rb))
	if err != nil {
		return err
	}

	_, err = c.DoRequest(req)
	return err
}

func (c *Client) GetAccounts() (*[]models.Account, error) {
	url := fmt.Sprintf("%s/accounts", c.HostURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	body, err := c.DoRequestWithRetry(req)
	if err != nil {
		return nil, err
	}

	account := []models.Account{}
	err = json.Unmarshal(body, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

func (c *Client) GetAccount(accountID string) (*models.Account, error) {
	url := fmt.Sprintf("%s/account?accountID=%s", c.HostURL, accountID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	body, err := c.DoRequestWithRetry(req)
	if err != nil {
		return nil, err
	}

	account := models.Account{}
	err = json.Unmarshal(body, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

func (c *Client) UpdateAccount(payload models.Payload) (*models.Account, error) {
	rb, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/account", c.HostURL)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(rb))
	if err != nil {
		return nil, err
	}

	body, err := c.DoRequest(req)
	if err != nil {
		return nil, err
	}

	account := models.Account{}
	err = json.Unmarshal(body, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}
