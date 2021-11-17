# flagger-k6-webhook

Using k6 to do load testing of the canary before rolling out traffic

## How to deploy

Deploy this as a Service + Deployment beside Flagger:

- Set the `K6_CLOUD_TOKEN` environment variable if any of your tests will be uploaded to [k6 cloud](https://k6.io/cloud/)
- Set the `SLACK_TOKEN` environment variable to allow slack updates

## Example

Here's what the `Canary` webhook can look like. This is `pre-rollout` webhook, so it happens before any traffic is placed on the canary. If the webhook passes the thresholds, the rest of the Flagger analysis and promotion process occurs

```yaml
apiVersion: flagger.app/v1beta1
kind: Canary
...
spec:
  analysis:
    ...
    webhooks:
    - name: k6-load-test
      timeout: 5m
      type: pre-rollout
      url: http://k6-loadtester.flagger/launch-test
      metadata:
        script: |
          import http from 'k6/http';
          import { sleep } from 'k6';
          export const options = {
            vus: 2,
            duration: '30s',
            thresholds: {
                http_req_duration: ['p(95)<50']
            },
            ext: {
              loadimpact: {
                name: '<cluster>/<your_service>',
                projectID: <project id>,
              },
            },
          };

          export default function () {
            http.get('http://<your_service>-canary.<namespace>:80/');
            sleep(0.10);
          }
        upload_to_cloud: "true"
        slack_channels: "channel1,channel2"
        notification_context: "My Cluster: `dev-us-east-1`" # Additional context to be added to the end of messages
        min_failure_delay: "2m" # Fail all successive runs after a failure (keyed to the namespace + name + phase) within the given duration (defaults to 2m). This prevents reruns. Set this to a duration slightly above the testing interval
        wait_for_results: "true" # Wait until the K6 analysis is completed before returning. This is required to fail/succeed on thresholds (defaults to true)
```
