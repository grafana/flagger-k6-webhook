package handlers

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewLaunchPayload(t *testing.T) {
	testCases := []struct {
		name    string
		request *http.Request
		want    *launchPayload
		wantErr error
	}{
		{
			name:    "no data",
			request: &http.Request{},
			wantErr: errors.New("no request body"),
		},
		{
			name: "invalid json",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader("bad")),
			},
			wantErr: errors.New("invalid character 'b' looking for beginning of value"),
		},
		{
			name: "missing base webhook attributes",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader("{}")),
			},
			wantErr: errors.New("error while validating base webhook: missing name"),
		},
		{
			name: "missing script",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {}}`)),
			},
			wantErr: errors.New("missing script"),
		},
		{
			name: "default values",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script"}}`)),
			},
			want: func() *launchPayload {
				p := &launchPayload{flaggerWebhook: flaggerWebhook{Name: "test", Namespace: "test", Phase: "pre-rollout"}}
				p.Metadata.Script = "my-script"
				p.Metadata.UploadToCloud = false
				p.Metadata.WaitForResults = true
				p.Metadata.SlackChannels = nil
				p.Metadata.MinFailureDelay = 2 * time.Minute
				return p
			}(),
		},
		{
			name: "set values",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{
					"name": "test", 
					"namespace": "test", 
					"phase": "pre-rollout", 
					"metadata": {
						"script": "my-script", 
						"upload_to_cloud": "true", 
						"wait_for_results": "false", 
						"slack_channels": "test,test2", 
						"min_failure_delay": "3m",
						"mount_kubernetes_secrets": "{\"TEST_VAR\": \"secret/key\"}"
					}
				}`)),
			},
			want: func() *launchPayload {
				p := &launchPayload{flaggerWebhook: flaggerWebhook{Name: "test", Namespace: "test", Phase: "pre-rollout"}}
				p.Metadata.Script = "my-script"
				p.Metadata.UploadToCloudString = "true"
				p.Metadata.UploadToCloud = true
				p.Metadata.WaitForResultsString = "false"
				p.Metadata.WaitForResults = false
				p.Metadata.SlackChannelsString = "test,test2"
				p.Metadata.SlackChannels = []string{"test", "test2"}
				p.Metadata.MinFailureDelay = 3 * time.Minute
				p.Metadata.MinFailureDelayString = "3m"
				p.Metadata.MountKubernetesSecrets = map[string]string{"TEST_VAR": "secret/key"}
				p.Metadata.MountKubernetesSecretsString = `{"TEST_VAR": "secret/key"}`
				return p
			}(),
		},
		{
			name: "invalid upload_to_cloud",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "bad"}}`)),
			},
			wantErr: errors.New(`error parsing value for 'upload_to_cloud': strconv.ParseBool: parsing "bad": invalid syntax`),
		},
		{
			name: "invalid wait_for_results",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script", "wait_for_results": "bad"}}`)),
			},
			wantErr: errors.New(`error parsing value for 'wait_for_results': strconv.ParseBool: parsing "bad": invalid syntax`),
		},
		{
			name: "invalid min_failure_delay",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script", "min_failure_delay": "bad"}}`)),
			},
			wantErr: errors.New(`error parsing value for 'min_failure_delay': time: invalid duration "bad"`),
		},
		{
			name: "invalid mount_kubernetes_secrets",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script", "mount_kubernetes_secrets": "[]"}}`)),
			},
			wantErr: errors.New(`error parsing value for 'mount_kubernetes_secrets': json: cannot unmarshal array into Go value of type map[string]string`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := newLaunchPayload(tc.request)
			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.want, payload)
		})
	}
}

func TestLaunchAndWaitCloud(t *testing.T) {
	// Initialize controller
	_, k6Client, _, slackClient, testRun, handler := setupHandler(t)

	// Expected calls
	// * Start the run
	fullResults, resultParts := getTestOutput(t)
	var bufferWriter io.Writer
	k6Client.EXPECT().Start("my-script", true, nil, gomock.Any()).DoAndReturn(func(scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		bufferWriter = outputWriter
		outputWriter.Write([]byte(resultParts[0]))
		return testRun, nil
	})

	// * Send the initial slack message
	channelMap := map[string]string{"C1234": "ts1", "C12345": "ts2"}
	slackClient.EXPECT().SendMessages(
		[]string{"test", "test2"},
		":warning: Load testing of `test-name` in namespace `test-space` has started",
		"extra context\nCloud URL: <https://app.k6.io/runs/1157843>",
	).Return(channelMap, nil)

	// * Wait for the command to finish
	testRun.EXPECT().Wait().DoAndReturn(func() error {
		bufferWriter.Write([]byte("running" + resultParts[1]))
		return nil
	})

	// * Upload the results file and update the slack message
	slackClient.EXPECT().AddFileToThreads(
		channelMap,
		"k6-results.txt",
		string(fullResults),
	).Return(nil)
	slackClient.EXPECT().UpdateMessages(
		channelMap,
		":large_green_circle: Load testing of `test-name` in namespace `test-space` has succeeded",
		"extra context\nCloud URL: <https://app.k6.io/runs/1157843>",
	).Return(nil)

	// Make request
	request := &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "true", "slack_channels": "test,test2", "notification_context": "extra context"}}`)),
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, fullResults, rr.Body.Bytes())
	assert.Equal(t, 200, rr.Result().StatusCode)
}

func TestLaunchAndWaitLocal(t *testing.T) {
	// Initialize controller
	_, k6Client, _, slackClient, testRun, handler := setupHandler(t)

	// Expected calls
	// * Start the run
	fullResults, resultParts := getTestOutput(t)
	var bufferWriter io.Writer
	k6Client.EXPECT().Start("my-script", false, nil, gomock.Any()).DoAndReturn(func(scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		bufferWriter = outputWriter
		outputWriter.Write([]byte(resultParts[0]))
		return testRun, nil
	}).Times(2)

	// * Send the initial slack message
	channelMap := map[string]string{"C1234": "ts1", "C12345": "ts2"}
	slackClient.EXPECT().SendMessages(
		[]string{"test", "test2"},
		":warning: Load testing of `test-name` in namespace `test-space` has started",
		"",
	).Times(2).Return(channelMap, nil)

	// * Wait for the command to finish
	testRun.EXPECT().Wait().Times(2).DoAndReturn(func() error {
		bufferWriter.Write([]byte("running" + resultParts[1]))
		return nil
	})

	// * Upload the results file and update the slack message
	slackClient.EXPECT().AddFileToThreads(
		channelMap,
		"k6-results.txt",
		string(fullResults),
	).Times(2).Return(nil)
	slackClient.EXPECT().UpdateMessages(
		channelMap,
		":large_green_circle: Load testing of `test-name` in namespace `test-space` has succeeded",
		"",
	).Times(2).Return(nil)

	// Make request
	request := &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "false", "slack_channels": "test,test2"}}`)),
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, fullResults, rr.Body.Bytes())
	assert.Equal(t, 200, rr.Result().StatusCode)

	//
	// Run it again immediately to see if we get the same result
	//

	// Make request
	request = &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "false", "slack_channels": "test,test2"}}`)),
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, fullResults, rr.Body.Bytes())
	assert.Equal(t, 200, rr.Result().StatusCode)
}

func TestLaunchAndWaitAndGetError(t *testing.T) {
	// Initialize controller
	_, k6Client, _, slackClient, testRun, handler := setupHandler(t)

	// Expected calls
	// * Start the run
	fullResults, resultParts := getTestOutput(t)
	var bufferWriter io.Writer
	k6Client.EXPECT().Start("my-script", false, nil, gomock.Any()).DoAndReturn(func(scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		bufferWriter = outputWriter
		outputWriter.Write([]byte(resultParts[0]))
		return testRun, nil
	})

	// * Send the initial slack message
	channelMap := map[string]string{"C1234": "ts1", "C12345": "ts2"}
	slackClient.EXPECT().SendMessages(
		[]string{"test", "test2"},
		":warning: Load testing of `test-name` in namespace `test-space` has started",
		"",
	).Return(channelMap, nil)

	// * Wait for the command to finish
	testRun.EXPECT().Wait().DoAndReturn(func() error {
		bufferWriter.Write([]byte("running" + resultParts[1]))
		return errors.New("exit code 1")
	})

	// * Upload the results file and update the slack message
	slackClient.EXPECT().AddFileToThreads(
		channelMap,
		"k6-results.txt",
		string(fullResults),
	).Return(nil)
	slackClient.EXPECT().UpdateMessages(
		channelMap,
		":red_circle: Load testing of `test-name` in namespace `test-space` has failed",
		"",
	).Return(nil)

	// Make request
	request := &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "false", "slack_channels": "test,test2"}}`)),
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, fmt.Sprintf("failed to run: exit code 1\n%s\n", string(fullResults)), rr.Body.String())
	assert.Equal(t, 400, rr.Result().StatusCode)

	//
	// Run it again immediately to get the failure due to min_failure_delay
	//

	// Make request
	request = &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "false", "slack_channels": "test,test2"}}`)),
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, "not enough time since last failure\n", rr.Body.String())
	assert.Equal(t, 400, rr.Result().StatusCode)
}

func TestLaunchNeverStarted(t *testing.T) {
	// Initialize controller
	_, k6Client, _, slackClient, testRun, handler := setupHandler(t)
	var sleepCalls []time.Duration
	sleepMock := func(d time.Duration) {
		sleepCalls = append(sleepCalls, d)
	}
	handler.sleep = sleepMock

	// Expected calls
	// * Start the run (process fails and prints out an error)
	k6Client.EXPECT().Start("my-script", false, nil, gomock.Any()).DoAndReturn(func(scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		outputWriter.Write([]byte("failed to run (k6 error)"))
		return testRun, nil
	})

	// * Upload the results file and send the error slack message
	channelMap := map[string]string{"C1234": "ts1", "C12345": "ts2"}
	slackClient.EXPECT().SendMessages(
		[]string{"test", "test2"},
		":red_circle: Load testing of `test-name` in namespace `test-space` didn't start successfully",
		"",
	).Return(channelMap, nil)
	slackClient.EXPECT().AddFileToThreads(
		channelMap,
		"k6-results.txt",
		"failed to run (k6 error)",
	).Return(nil)

	// Make request
	request := &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "upload_to_cloud": "false", "slack_channels": "test,test2"}}`)),
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, "error while waiting for test to start: timeout\nfailed to run (k6 error)\n", rr.Body.String())
	assert.Equal(t, 400, rr.Result().StatusCode)
	// 10 sleep calls
	assert.Equal(t, sleepCalls, []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second,
		2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second})
}

func TestLaunchWithoutWaiting(t *testing.T) {
	// Initialize controller
	_, k6Client, _, slackClient, testRun, handler := setupHandler(t)

	// Expected calls
	// * Start the run
	_, resultParts := getTestOutput(t)
	k6Client.EXPECT().Start("my-script", false, nil, gomock.Any()).DoAndReturn(func(scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		outputWriter.Write([]byte(resultParts[0]))
		return testRun, nil
	})

	// * Send the initial slack message (process ends here)
	channelMap := map[string]string{"C1234": "ts1", "C12345": "ts2"}
	slackClient.EXPECT().SendMessages(
		[]string{"test", "test2"},
		":warning: Load testing of `test-name` in namespace `test-space` has started",
		"",
	).Return(channelMap, nil)

	// Make request
	request := &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{"name": "test-name", "namespace": "test-space", "phase": "pre-rollout", "metadata": {"script": "my-script", "wait_for_results": "false", "slack_channels": "test,test2"}}`)),
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, "", rr.Body.String())
	assert.Equal(t, 200, rr.Result().StatusCode)
}

func setupHandler(t *testing.T) (*gomock.Controller, *mocks.MockK6Client, *fake.Clientset, *mocks.MockSlackClient, *mocks.MockK6TestRun, *launchHandler) {
	t.Helper()

	mockCtrl := gomock.NewController(t)
	k6Client := mocks.NewMockK6Client(mockCtrl)
	kubeClient := fake.NewSimpleClientset()
	slackClient := mocks.NewMockSlackClient(mockCtrl)
	testRun := mocks.NewMockK6TestRun(mockCtrl)
	handler, err := NewLaunchHandler(k6Client, kubeClient, slackClient)
	handler.(*launchHandler).sleep = func(d time.Duration) {}
	require.NoError(t, err)

	return mockCtrl, k6Client, kubeClient, slackClient, testRun, handler.(*launchHandler)
}

func getTestOutput(t *testing.T) ([]byte, []string) {
	t.Helper()

	fullResults, err := os.ReadFile("testdata/k6-output.txt")
	require.NoError(t, err)
	resultParts := strings.SplitN(string(fullResults), "running", 2)

	return fullResults, resultParts
}
