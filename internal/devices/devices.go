package devices

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/vinistoisr/zerotrust-exporter/internal/appmetrics"
	"github.com/vinistoisr/zerotrust-exporter/internal/config"
)

type DeviceStatus struct {
	Colo        string `json:"colo"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	Platform    string `json:"platform"`
	Version     string `json:"version"`
	Timestamp   string `json:"timestamp"`
	DeviceName  string `json:"deviceName"`
	DeviceID    string `json:"deviceId"`
	PersonEmail string `json:"personEmail"`
}

func fetchDeviceStatus(ctx context.Context, accountID string) (map[string]DeviceStatus, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/dex/fleet-status/devices", accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		appmetrics.IncApiErrorsCounter()
		appmetrics.SetUpMetric(0)
		return nil, err
	}
	// add authorization headers
	req.Header.Set("Authorization", "Bearer "+config.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	// define query parameters
	q := req.URL.Query()
	q.Add("per_page", "50")
	q.Add("page", "1")
	q.Add("time_end", time.Unix(time.Now().Unix(), 0).Format(time.RFC3339))
	q.Add("time_start", time.Unix(time.Now().Add(-time.Minute*10).Unix(), 0).Format(time.RFC3339))
	q.Add("sort_by", "device_id")
	q.Add("status", "connected")
	q.Add("source", "last_seen")
	// add query parameters to the request
	req.URL.RawQuery = q.Encode()
	// send the request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	// defer closing the response body
	defer resp.Body.Close()
	// increment the api call counter
	appmetrics.IncApiCallCounter()
	// parse the status code if not ok
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		return nil, fmt.Errorf("failed to fetch device status: %s, response body: %s", resp.Status, bodyString)
	}
	// parse the response body into a struct
	var response struct {
		Result []DeviceStatus `json:"result"`
	}
	// decode the response body
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	deviceStatuses := make(map[string]DeviceStatus)
	for _, deviceStatus := range response.Result {
		deviceStatuses[deviceStatus.DeviceID] = deviceStatus
	}

	return deviceStatuses, nil
}

func CollectDeviceMetrics() map[string]DeviceStatus {
	appmetrics.IncApiCallCounter()
	ctx := context.Background()
	startTime := time.Now()

	deviceStatuses, err := fetchDeviceStatus(ctx, config.AccountID)
	if err != nil {
		log.Printf("Error fetching device status: %v", err)
		appmetrics.IncApiErrorsCounter()
		appmetrics.SetUpMetric(0)
		return nil
	}

	if config.Debug {
		log.Printf("Fetched %d devices in %v", len(deviceStatuses), time.Since(startTime))
	}

	filteredDevices := make(map[string]DeviceStatus)
	for _, status := range deviceStatuses {
		if status.Status == "connected" {
			filteredDevices[status.DeviceID] = status
		}
	}

	for deviceID, status := range filteredDevices {
		metricName := fmt.Sprintf(`zerotrust_devices_up{device_id="%s", device_name="%s", user_email="%s", colo="%s", mode="%s", platform="%s", version="%s"}`, deviceID, status.DeviceName, status.PersonEmail, status.Colo, status.Mode, status.Platform, status.Version)
		gauge := metrics.GetOrCreateGauge(metricName, nil)
		gauge.Set(1)
	}

	log.Println("Device metrics collection completed.")
	return filteredDevices
}
