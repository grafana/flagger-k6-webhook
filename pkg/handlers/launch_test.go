package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/mocks"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
						"kubernetes_secrets": "{\"TEST_VAR\": \"secret/key\"}",
						"env_vars": "{\"TEST_VAR2\": \"value\"}"
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
				p.Metadata.KubernetesSecrets = map[string]string{"TEST_VAR": "secret/key"}
				p.Metadata.KubernetesSecretsString = `{"TEST_VAR": "secret/key"}`
				p.Metadata.EnvVars = map[string]string{"TEST_VAR2": "value"}
				p.Metadata.EnvVarsString = `{"TEST_VAR2": "value"}`
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
			name: "invalid kubernetes_secrets",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script", "kubernetes_secrets": "[]"}}`)),
			},
			wantErr: errors.New(`error parsing value for 'kubernetes_secrets': json: cannot unmarshal array into Go value of type map[string]string`),
		},
		{
			name: "invalid env_vars",
			request: &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(`{"name": "test", "namespace": "test", "phase": "pre-rollout", "metadata": {"script": "my-script", "env_vars": "[]"}}`)),
			},
			wantErr: errors.New(`error parsing value for 'env_vars': json: cannot unmarshal array into Go value of type map[string]string`),
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
	tests := map[string]struct {
		k6OutputFile string
		cloudURL     string
	}{
		"legacy-cloud-url": {
			k6OutputFile: "testdata/k6-output-legacy.txt",
			cloudURL:     "https://app.k6.io/runs/1157843",
		},
		"grafana-cloud-url": {
			k6OutputFile: "testdata/k6-output.txt",
			cloudURL:     "https://somewhere.grafana.net/a/k6-app/runs/1157843",
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			// Initialize controller
			_, cancel, _, k6Client, slackClient, testRun, handler := setupHandler(t, 100)
			t.Cleanup(handler.Wait)
			t.Cleanup(cancel)

			// Expected calls
			// * Start the run
			fullResults, resultParts := getTestOutputFromFile(t, test.k6OutputFile)
			var bufferWriter io.Writer
			k6Client.EXPECT().Start(gomock.Any(), "my-script", true, nil, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
				bufferWriter = outputWriter
				outputWriter.Write([]byte(resultParts[0]))
				return testRun, nil
			})

			// * Send the initial slack message
			channelMap := map[string]string{"C1234": "ts1", "C12345": "ts2"}
			slackClient.EXPECT().SendMessages(
				[]string{"test", "test2"},
				":warning: Load testing of `test-name` in namespace `test-space` has started",
				fmt.Sprintf("extra context\nCloud URL: <%s>", test.cloudURL),
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
				fmt.Sprintf("extra context\nCloud URL: <%s>", test.cloudURL),
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
		})
	}
}

func TestSlackFailuresDontAbort(t *testing.T) {
	// Initialize controller
	_, cancel, _, k6Client, slackClient, testRun, handler := setupHandler(t, 100)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	// Expected calls
	// * Start the run
	fullResults, resultParts := getTestOutput(t)
	var bufferWriter io.Writer
	k6Client.EXPECT().Start(gomock.Any(), "my-script", true, nil, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		bufferWriter = outputWriter
		outputWriter.Write([]byte(resultParts[0]))
		return testRun, nil
	})

	// * Send the initial slack message
	slackClient.EXPECT().SendMessages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("error sending message"))

	// * Wait for the command to finish
	testRun.EXPECT().Wait().DoAndReturn(func() error {
		bufferWriter.Write([]byte("running" + resultParts[1]))
		return nil
	})

	// * Upload the results file and update the slack message
	slackClient.EXPECT().AddFileToThreads(nil, "k6-results.txt", string(fullResults)).Return(errors.New("error adding file"))
	slackClient.EXPECT().UpdateMessages(nil, gomock.Any(), gomock.Any()).Return(errors.New("error updating message"))

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
	_, cancel, _, k6Client, slackClient, testRun, handler := setupHandler(t, 100)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	// Expected calls
	// * Start the run
	fullResults, resultParts := getTestOutput(t)
	var bufferWriter io.Writer
	k6Client.EXPECT().Start(gomock.Any(), "my-script", false, nil, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
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
	_, cancel, _, k6Client, slackClient, testRun, handler := setupHandler(t, 100)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	// Expected calls
	// * Start the run
	fullResults, resultParts := getTestOutput(t)
	var bufferWriter io.Writer
	k6Client.EXPECT().Start(gomock.Any(), "my-script", false, nil, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
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
	_, cancel, _, k6Client, slackClient, testRun, handler := setupHandler(t, 100)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	testRun.EXPECT().PID().Return(-1).AnyTimes()
	testRun.EXPECT().Kill().Return(nil).AnyTimes()
	testRun.EXPECT().Wait().Return(nil).AnyTimes()
	testRun.EXPECT().Exited().Return(true).AnyTimes()

	var sleepCalls []time.Duration
	sleepMock := func(d time.Duration) {
		sleepCalls = append(sleepCalls, d)
	}
	handler.sleep = sleepMock

	// Expected calls
	// * Start the run (process fails and prints out an error)
	k6Client.EXPECT().Start(gomock.Any(), "my-script", false, nil, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
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
	_, cancel, _, k6Client, slackClient, testRun, handler := setupHandler(t, 100)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	testRun.EXPECT().PID().Return(-1).AnyTimes()
	testRun.EXPECT().Kill().Return(nil).AnyTimes()
	testRun.EXPECT().Wait().Return(nil).AnyTimes()
	testRun.EXPECT().Exited().Return(true).AnyTimes()

	// Expected calls
	// * Start the run
	_, resultParts := getTestOutput(t)
	k6Client.EXPECT().Start(gomock.Any(), "my-script", false, nil, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
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

func TestBadPayload(t *testing.T) {
	// Initialize controller
	_, cancel, _, _, _, _, handler := setupHandler(t, 100)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	// Make request
	request := &http.Request{
		Body: ioutil.NopCloser(strings.NewReader(`{}`)),
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	// Expected response
	assert.Equal(t, "error while validating request: error while validating base webhook: missing name\n", rr.Body.String())
	assert.Equal(t, 400, rr.Result().StatusCode)
}

func TestEnvVars(t *testing.T) {
	fullResults, resultParts := getTestOutput(t)

	for _, tc := range []struct {
		name              string
		secretsSetting    string
		envVarsSetting    string
		kubernetesObjects []runtime.Object
		nilKubeClient     bool
		expected          string
		expectedEnvVars   map[string]string
		expectedCode      int
	}{
		{
			name:         "no secrets",
			expected:     string(fullResults),
			expectedCode: 200,
		},
		{
			name:            "direct env vars",
			envVarsSetting:  `{\"FOO\": \"bar\", \"BAZ\": \"qux\"}`,
			expected:        string(fullResults),
			expectedEnvVars: map[string]string{"FOO": "bar", "BAZ": "qux"},
			expectedCode:    200,
		},
		{
			name:           "working example",
			secretsSetting: `{\"TEST_VAR\": \"other-namespace/secret-name/secret-key\"}`,
			kubernetesObjects: []runtime.Object{
				&v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-name", Namespace: "other-namespace"}, Type: "Opaque", Data: map[string][]byte{"secret-key": []byte("secret-value")}},
			},
			expected:        string(fullResults),
			expectedEnvVars: map[string]string{"TEST_VAR": "secret-value"},
			expectedCode:    200,
		},
		{
			name:           "both env vars and secrets",
			envVarsSetting: `{\"FOO\": \"bar\", \"BAZ\": \"qux\"}`,
			secretsSetting: `{\"TEST_VAR\": \"other-namespace/secret-name/secret-key\"}`,
			kubernetesObjects: []runtime.Object{
				&v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-name", Namespace: "other-namespace"}, Type: "Opaque", Data: map[string][]byte{"secret-key": []byte("secret-value")}},
			},
			expected:        string(fullResults),
			expectedEnvVars: map[string]string{"FOO": "bar", "BAZ": "qux", "TEST_VAR": "secret-value"},
			expectedCode:    200,
		},
		{
			name:           "no given namespace (defaults to the payload namespace)",
			secretsSetting: `{\"TEST_VAR\": \"secret-name/secret-key\"}`,
			kubernetesObjects: []runtime.Object{
				&v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-name", Namespace: "test-space"}, Type: "Opaque", Data: map[string][]byte{"secret-key": []byte("secret-value")}},
			},
			expected:        string(fullResults),
			expectedEnvVars: map[string]string{"TEST_VAR": "secret-value"},
			expectedCode:    200,
		},
		{
			name:           "missing secret",
			secretsSetting: `{\"TEST_VAR\": \"secret-name/secret-key\"}`,
			expected:       "error fetching secret test-space/secret-name: secrets \"secret-name\" not found\n",
			expectedCode:   400,
		},
		{
			name:           "missing secret key",
			secretsSetting: `{\"TEST_VAR\": \"secret-name/secret-key\"}`,
			kubernetesObjects: []runtime.Object{
				&v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-name", Namespace: "test-space"}, Type: "Opaque", Data: map[string][]byte{"other-key": []byte("secret-value")}},
			},
			expected:     "secret test-space/secret-name does not have key secret-key\n",
			expectedCode: 400,
		},
		{
			name:           "no kube client",
			secretsSetting: `{\"TEST_VAR\": \"secret-name/secret-key\"}`,
			expected:       "kubernetes client is not configured\n",
			expectedCode:   400,
			nilKubeClient:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize controller
			_, cancel, _, k6Client, slackClient, testRun, handler := setupHandlerWithKubernetesObjects(t, 100, tc.kubernetesObjects...)
			if tc.nilKubeClient {
				handler.kubeClient = nil
			}
			t.Cleanup(handler.Wait)
			t.Cleanup(cancel)

			if tc.expectedCode == 200 {
				// Expected calls
				// * Start the run
				var bufferWriter io.Writer
				k6Client.EXPECT().Start(gomock.Any(), "my-script", false, tc.expectedEnvVars, gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
					bufferWriter = outputWriter
					outputWriter.Write([]byte(resultParts[0]))
					return testRun, nil
				})

				// * Send the initial slack message (to no channels)
				slackClient.EXPECT().SendMessages(nil, gomock.Any(), "").Return(nil, nil)

				// * Wait for the command to finish
				testRun.EXPECT().Wait().DoAndReturn(func() error {
					bufferWriter.Write([]byte("running" + resultParts[1]))
					return nil
				})

				// * Upload the results file and update the slack message (to no channels)
				slackClient.EXPECT().AddFileToThreads(nil, "k6-results.txt", string(fullResults)).Return(nil)
				slackClient.EXPECT().UpdateMessages(nil, gomock.Any(), "").Return(nil)
			}

			// Make request
			request := &http.Request{
				Body: ioutil.NopCloser(strings.NewReader(fmt.Sprintf(`{
					"name": "test-name",
					"namespace": "test-space",
					"phase": "pre-rollout",
					"metadata": {
						"script": "my-script",
						"kubernetes_secrets": "%s",
						"env_vars": "%s"
					}
				}`, tc.secretsSetting, tc.envVarsSetting))),
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, request)

			// Expected response
			assert.Equal(t, tc.expected, rr.Body.String())
			assert.Equal(t, tc.expectedCode, rr.Result().StatusCode)
		})
	}

}

func TestProcessHandler(t *testing.T) {
	t.Run("waits on processes", func(t *testing.T) {
		logrus.SetLevel(logrus.DebugLevel)
		_, cancel, ctrl, _, _, _, handler := setupHandler(t, 100)
		// Now let's produce a handful of test runs and check that they are waited
		// on
		for range 10 {
			<-handler.availableTestRuns
			tr := mocks.NewMockK6TestRun(ctrl)
			tr.EXPECT().PID().Return(-1).AnyTimes()
			tr.EXPECT().Kill().Return(nil).AnyTimes()
			// Wait is called exactly once by the process handler
			tr.EXPECT().Wait().Return(nil).Times(1)
			tr.EXPECT().Exited().Return(true).AnyTimes()
			tr.EXPECT().ExitCode().Return(0).AnyTimes()
			tr.EXPECT().SetCancelFunc(gomock.Any()).Return().AnyTimes()
			tr.EXPECT().CleanupContext().Return().AnyTimes()
			tr.EXPECT().ExecutionDuration().Return(time.Minute).AnyTimes()
			handler.registerProcessCleanup(tr)
		}
		time.Sleep(time.Second * 2)
		t.Log("Cancelling handler")
		cancel()
		handler.Wait()
	})

	t.Run("kills process if handler is closed", func(t *testing.T) {
		logrus.SetLevel(logrus.DebugLevel)
		ctx, cancelCtx, _, _, _, _, handler := setupHandler(t, 100)
		cmd := exec.CommandContext(ctx, "sleep", "10")
		require.NoError(t, cmd.Start())
		<-handler.availableTestRuns
		handler.registerProcessCleanup(&k6.DefaultTestRun{Cmd: cmd})

		// Also register a process that will be done by the time we are closing
		// the handler:
		cmdSuccess := exec.Command("true")
		require.NoError(t, cmdSuccess.Start())
		<-handler.availableTestRuns
		handler.registerProcessCleanup(&k6.DefaultTestRun{Cmd: cmdSuccess})

		// Yield so that the handler can actually pick up the process:
		time.Sleep(time.Second)

		cancelCtx()
		handler.Wait()
		require.False(t, cmd.ProcessState.Success())
		require.True(t, cmdSuccess.ProcessState.Success())
	})
}

// If we get too many concurrent test requests, a 429 should be returned by the
// ServeHTTP method.
func Test429OnExcessiveRequests(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	// Initialize controller
	_, cancel, ctrl, k6Client, slackClient, testRun1, handler := setupHandler(t, 1)
	t.Cleanup(handler.Wait)
	t.Cleanup(cancel)

	slackClient.EXPECT().SendMessages(nil, gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	slackClient.EXPECT().AddFileToThreads(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	_, resultParts := getTestOutputFromFile(t, "testdata/k6-output.txt")

	// The first request should go through but the second should be rejected
	// with a 429 response as it would exceed the concurrent testrun limit:
	request1 := &http.Request{
		Body: io.NopCloser(strings.NewReader(`{"name": "hello", "namespace": "default", "phase": "somephase", "metadata": {"upload_to_cloud": "false", "wait_for_results": "false", "script": "import { sleep } from 'k6'; export default function() { sleep(10) }"}}`)),
	}

	var bufferWriter1 io.Writer
	k6Client.EXPECT().Start(gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, scriptContent string, upload bool, envVars map[string]string, outputWriter io.Writer) (k6.TestRun, error) {
		bufferWriter1 = outputWriter
		outputWriter.Write([]byte(resultParts[0]))
		return testRun1, nil
	}).AnyTimes()
	testRun1.EXPECT().PID().Return(-1).AnyTimes()
	testRun1.EXPECT().Wait().DoAndReturn(func() error {
		time.Sleep(time.Second * 2)
		bufferWriter1.Write([]byte("running" + resultParts[1]))
		return nil
	}).AnyTimes()
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, request1)
	require.Equal(t, 200, rr1.Code)

	testRun2 := mocks.NewMockK6TestRun(ctrl)
	request2 := &http.Request{
		Body: io.NopCloser(strings.NewReader(`{"name": "hello", "namespace": "default", "phase": "somephase", "metadata": {"upload_to_cloud": "false", "wait_for_results": "false", "script": "import { sleep } from 'k6'; export default function() { sleep(10) }"}}`)),
	}

	// All these mock calls should actually never happen as the request is rejected right away
	k6Client.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	testRun2.EXPECT().PID().Return(-1).Times(0)
	testRun2.EXPECT().Wait().Times(0)

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, request2)
	require.Equal(t, 429, rr2.Code)
}

func setupHandler(t *testing.T, maxConcurrentTests int) (context.Context, context.CancelFunc, *gomock.Controller, *mocks.MockK6Client, *mocks.MockSlackClient, *mocks.MockK6TestRun, *launchHandler) {
	return setupHandlerWithKubernetesObjects(t, maxConcurrentTests)
}

func setupHandlerWithKubernetesObjects(t *testing.T, maxConcurrentTests int, expectedKubernetesObjects ...runtime.Object) (context.Context, context.CancelFunc, *gomock.Controller, *mocks.MockK6Client, *mocks.MockSlackClient, *mocks.MockK6TestRun, *launchHandler) {
	t.Helper()

	mockCtrl := gomock.NewController(t)
	k6Client := mocks.NewMockK6Client(mockCtrl)
	kubeClient := fake.NewSimpleClientset(expectedKubernetesObjects...)
	slackClient := mocks.NewMockSlackClient(mockCtrl)
	testRun := mocks.NewMockK6TestRun(mockCtrl)

	// For now we do not test the ExecutionDuration and so can set a default
	// value here:
	testRun.EXPECT().ExecutionDuration().Return(time.Minute).AnyTimes()
	testRun.EXPECT().ExitCode().Return(0).AnyTimes()
	testRun.EXPECT().SetCancelFunc(gomock.Any()).Return().AnyTimes()
	testRun.EXPECT().CleanupContext().Return().AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	handler, err := NewLaunchHandler(ctx, k6Client, kubeClient, slackClient, maxConcurrentTests)
	handler.(*launchHandler).sleep = func(d time.Duration) {}
	require.NoError(t, err)

	return ctx, cancel, mockCtrl, k6Client, slackClient, testRun, handler.(*launchHandler)
}

func getTestOutput(t *testing.T) ([]byte, []string) {
	t.Helper()

	return getTestOutputFromFile(t, "testdata/k6-output.txt")
}

func getTestOutputFromFile(t *testing.T, filename string) ([]byte, []string) {
	t.Helper()

	fullResults, err := os.ReadFile(filename)
	require.NoError(t, err)
	resultParts := strings.SplitN(string(fullResults), "running", 2)

	return fullResults, resultParts
}
