package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/flagger-k6-webhook/pkg/k6"
	"github.com/grafana/flagger-k6-webhook/pkg/slack"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

const (
	emojiSuccess = ":large_green_circle:"
	emojiWarning = ":warning:"
	emojiFailure = ":red_circle:"

	metricTestDurationName = "launch_test_duration"
)

// https://regex101.com/r/OZwd8Y/1
var outputRegex = regexp.MustCompile(`output: cloud \((?P<url>https:\/\/((app\.k6\.io)|([^/]+\.grafana.net\/a\/k6-app))\/runs\/\d+)\)`)

type launchPayload struct {
	flaggerWebhook
	Metadata struct {
		Script string `json:"script"`

		// If true, the test results will be uploaded to cloud
		UploadToCloudString string `json:"upload_to_cloud"`
		UploadToCloud       bool

		// If true, the handler will wait for the k6 run to be completed
		WaitForResultsString string `json:"wait_for_results"`
		WaitForResults       bool

		// Notification settings. Context is added at the end of the message
		SlackChannelsString string `json:"slack_channels"`
		SlackChannels       []string
		NotificationContext string `json:"notification_context"`

		// Min delay between failures. All other runs will fail immediately. This prevents retries
		MinFailureDelay       time.Duration
		MinFailureDelayString string `json:"min_failure_delay"`

		// Set environment variables when running the k6 script
		EnvVars       map[string]string
		EnvVarsString string `json:"env_vars"`

		// Inject secrets to environment (map of `<ENV>` -> `<namespace (default: payload namespace)>/<secret name>/<secret key>`)
		KubernetesSecrets       map[string]string
		KubernetesSecretsString string `json:"kubernetes_secrets"`
	} `json:"metadata"`
}

func (p *launchPayload) statusMessage(emoji, status string) string {
	return fmt.Sprintf("%s Load testing of `%s` in namespace `%s` %s", emoji, p.Name, p.Namespace, status)
}

func (p *launchPayload) key() string {
	return fmt.Sprintf("%s-%s-%s", p.Namespace, p.Name, p.Phase)
}

func newLaunchPayload(req *http.Request) (*launchPayload, error) {
	var err error
	payload := &launchPayload{}

	if req.Body == nil {
		return nil, errors.New("no request body")
	}
	defer req.Body.Close()
	if err = json.NewDecoder(req.Body).Decode(payload); err != nil {
		return nil, err
	}

	if err := payload.validateBaseWebhook(); err != nil {
		return nil, fmt.Errorf("error while validating base webhook: %w", err)
	}

	if err := payload.validate(); err != nil {
		return nil, err
	}

	return payload, nil
}

func (p *launchPayload) validate() error {
	var err error

	if p.Metadata.Script == "" {
		return errors.New("missing script")
	}

	if p.Metadata.UploadToCloudString == "" {
		p.Metadata.UploadToCloud = false
	} else if p.Metadata.UploadToCloud, err = strconv.ParseBool(p.Metadata.UploadToCloudString); err != nil {
		return fmt.Errorf("error parsing value for 'upload_to_cloud': %w", err)
	}

	if p.Metadata.WaitForResultsString == "" {
		p.Metadata.WaitForResults = true
	} else if p.Metadata.WaitForResults, err = strconv.ParseBool(p.Metadata.WaitForResultsString); err != nil {
		return fmt.Errorf("error parsing value for 'wait_for_results': %w", err)
	}

	if p.Metadata.SlackChannelsString != "" {
		p.Metadata.SlackChannels = strings.Split(p.Metadata.SlackChannelsString, ",")
	}

	if p.Metadata.MinFailureDelayString == "" {
		p.Metadata.MinFailureDelay = 2 * time.Minute
	} else if p.Metadata.MinFailureDelay, err = time.ParseDuration(p.Metadata.MinFailureDelayString); err != nil {
		return fmt.Errorf("error parsing value for 'min_failure_delay': %w", err)
	}

	if p.Metadata.EnvVarsString != "" {
		if err := json.Unmarshal([]byte(p.Metadata.EnvVarsString), &p.Metadata.EnvVars); err != nil {
			return fmt.Errorf("error parsing value for 'env_vars': %w", err)
		}
	}

	if p.Metadata.KubernetesSecretsString != "" {
		if err := json.Unmarshal([]byte(p.Metadata.KubernetesSecretsString), &p.Metadata.KubernetesSecrets); err != nil {
			return fmt.Errorf("error parsing value for 'kubernetes_secrets': %w", err)
		}
	}

	return nil
}

// launchHandler is responsible for receiving new requests and dispatching a
// singleRequestHandler based on the received payload. It also keeps track of
// all currently running processes.
type launchHandler struct {
	client      k6.Client
	kubeClient  kubernetes.Interface
	slackClient slack.Client

	lastFailureTime      map[string]time.Time
	lastFailureTimeMutex sync.Mutex

	processToWaitFor     chan k6.TestRun
	waitForProcessesDone chan struct{}
	ctx                  context.Context

	availableTestRuns chan struct{}

	metricsRegistry    *prometheus.Registry
	metricTestDuration *prometheus.SummaryVec

	// mockables
	sleep func(time.Duration)
}

type LaunchHandler interface {
	http.Handler
	Wait()
}

// NewLaunchHandler returns an handler that launches a k6 load test.
func NewLaunchHandler(ctx context.Context, client k6.Client, kubeClient kubernetes.Interface, slackClient slack.Client, maxConcurrentTests int) (LaunchHandler, error) {
	if slackClient == nil {
		return nil, errors.New("unexpected state. Slack client is nil")
	}

	h := &launchHandler{
		client:               client,
		kubeClient:           kubeClient,
		slackClient:          slackClient,
		lastFailureTime:      make(map[string]time.Time),
		sleep:                time.Sleep,
		processToWaitFor:     make(chan k6.TestRun, maxConcurrentTests),
		waitForProcessesDone: make(chan struct{}, 1),
		ctx:                  ctx,
	}
	h.availableTestRuns = make(chan struct{}, maxConcurrentTests)
	for range maxConcurrentTests {
		h.releaseTestRun()
	}

	metricMaxConcurrentTests := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "launch_max_concurrent_tests",
		Help: "The maximum number of concurrent tests",
	})
	metricMaxConcurrentTests.Set(float64(maxConcurrentTests))
	if err := prometheus.Register(metricMaxConcurrentTests); err != nil {
		log.Warnf("Failed to register new metric: %s", err.Error())
	}

	metricAvailableConcurrentTests := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "launch_available_concurrent_tests",
		Help: "The current number of available concurrent tests. If 0 then new requests will be rejected",
	}, func() float64 {
		return float64(len(h.availableTestRuns))
	})
	if err := prometheus.Register(metricAvailableConcurrentTests); err != nil {
		log.Warnf("Failed to register new metric: %s", err.Error())
	}

	// metricTestDuration is an internal metric that we use to calculate the
	// expected wait time in case the maximum number of concurrent tests is
	// reached:
	metricTestDuration := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name:       metricTestDurationName,
		Help:       "Durations of the executed k6 test run in seconds",
		Objectives: map[float64]float64{0.5: float64(30)},
	}, []string{"exit_code"})
	h.metricTestDuration = metricTestDuration
	h.metricsRegistry = prometheus.NewRegistry()
	_ = h.metricsRegistry.Register(h.metricTestDuration)

	go h.waitForProcesses(ctx)
	return h, nil
}

// Wait is blocking until all subprocesses have terminated. This should only be
// used if the passed context can (and is) canceled.
func (h *launchHandler) Wait() {
	<-h.waitForProcessesDone
	log.Debug("launch handler finished")
}

// waitForProcesses handles incoming processes and waits for them to complete.
// This way we can avoid k6 jobs where we do not need the results to become
// zombie processes.
func (h *launchHandler) waitForProcesses(ctx context.Context) {
	defer func() {
		h.waitForProcessesDone <- struct{}{}
	}()
	wg := sync.WaitGroup{}
loop:
	for {
		select {
		case cmd := <-h.processToWaitFor:
			wg.Add(1)
			go func() {
				h.waitForProcess(cmd)
				wg.Done()
			}()
		case <-ctx.Done():
			break loop
		}
	}
	wg.Wait()
}

func (h *launchHandler) waitForProcess(cmd k6.TestRun) {
	if cmd == nil {
		log.Warnf("nil as testrun passed")
		return
	}
	pid := cmd.PID()
	log.WithField("pid", pid).Debug("waiting for testrun to exit")
	_ = cmd.Wait()
	h.trackExecutionDuration(cmd)
	log.WithField("pid", pid).Debugf("testrun exited")

	// Also clean up the context attached to this process if present:
	cmd.CleanupContext()

	h.releaseTestRun()
}

// registerProcessCleanup adds a handler to the process so that it will
// eventually be closed and its resources returned.
//
// Note that this method can actually block which will, in turn, cause the
// calling HTTP handler to be blocked.
func (h *launchHandler) registerProcessCleanup(cmd k6.TestRun) {
	h.processToWaitFor <- cmd
}

func (h *launchHandler) getLastFailureTime(payload *launchPayload) (time.Time, bool) {
	h.lastFailureTimeMutex.Lock()
	defer h.lastFailureTimeMutex.Unlock()
	v, ok := h.lastFailureTime[payload.key()]
	return v, ok
}

func (h *launchHandler) setLastFailureTime(payload *launchPayload) {
	h.lastFailureTimeMutex.Lock()
	defer h.lastFailureTimeMutex.Unlock()
	h.lastFailureTime[payload.key()] = time.Now()
}

func (h *launchHandler) getWaitTime() int64 {
	families, err := h.metricsRegistry.Gather()
	if err != nil {
		return 60
	}
	for _, family := range families {
		if family.GetName() == metricTestDurationName {
			for _, metric := range family.GetMetric() {
				for _, quantile := range metric.GetSummary().GetQuantile() {
					if quantile.GetQuantile() == 0.5 {
						result := quantile.GetValue()
						return int64(result)
					}
				}
			}
		}
	}
	return 60
}

func (h *launchHandler) requestTestRun() error {
	select {
	case <-h.availableTestRuns:
		return nil
	default:
		return fmt.Errorf("maximum concurrent test runs reached")
	}
}

func (h *launchHandler) releaseTestRun() {
	h.availableTestRuns <- struct{}{}
}

func (h *launchHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	handler := newSingleRequestHandler(resp, req, h)
	handler.Handle(req.Context())
}

func (h *launchHandler) trackExecutionDuration(cmd k6.TestRun) {
	if dur := cmd.ExecutionDuration(); dur != 0 {
		h.metricTestDuration.With(prometheus.Labels{"exit_code": fmt.Sprintf("%d", cmd.ExitCode())}).Observe(float64(dur / time.Second))
	}
}
