if k8s_context() != 'kind-kind':
  fail("failing early to avoid overwriting prod")


def name(res):
    return res['metadata']['name']


def download_to_cache(url, filename):
    full_path = os.path.join('.cache', filename)
    if not os.path.exists(full_path):
        local('curl --create-dirs --location --output %s %s' % (full_path, url))
    return full_path


load('ext://secret', 'secret_from_dict')
load('ext://helm_resource', 'helm_resource', 'helm_repo')

helm_repo('helm-traefik',  'https://traefik.github.io/charts')
helm_repo('helm-flagger',  'https://flagger.app')

# Download the standard gateway CRD set
gateway_api = read_file(
    download_to_cache(
        'https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml',
        'gateway-crds.yaml'
    )
)
k8s_yaml(gateway_api)

# Now let let's see all the CRDs defined here and bundle them up
crds = [res for res in decode_yaml_stream(gateway_api) if res['kind'] == 'CustomResourceDefinition']
k8s_resource(new_name='gateway-crds', objects=[name(crd) for crd in crds], labels='gateway')

local_resource(
  'gateway-crds-ready',
  cmd=' && '.join([('kubectl --context kind-kind wait --for=condition=Established crd %s' % name(c)) for c in crds]),
  resource_deps=['gateway-crds'], labels='gateway')

# Once the CRDs are ready, we can also load rbac
gateway_rbac = read_file(
    download_to_cache(
        'https://raw.githubusercontent.com/traefik/traefik/v3.2/docs/content/reference/dynamic-configuration/kubernetes-gateway-rbac.yml',
        'gateway-rbac.yml',
    )
)
k8s_yaml(gateway_rbac)
k8s_resource(new_name='gateway-rbac', objects=[
    name(res) for res in decode_yaml_stream(gateway_rbac)
], resource_deps=['gateway-crds-ready'], labels='gateway')

# Use traefik as service mesh and ingress
helm_resource('traefik', 'helm-traefik/traefik', flags=[
    '--set=image.tag=v3.2.3',
    '--set=providers.kubernetesGateway.enabled=true',
    '--set=gateway.enabled=true',
    '--set=gateway.listeners.web.namespacePolicy=null',
], resource_deps=['gateway-rbac'], port_forwards=['8000:8000'], labels='gateway')

helm_resource('flagger', 'helm-flagger/flagger', flags=[
    '--set=prometheus.install=false',
    '--set=meshProvider=gatewayapi:v1',
], resource_deps=['traefik'], labels='flagger')

# Now let's wait until the Canary CRD is ready
local_resource(
  'flagger-crds-ready',
  cmd='kubectl --context kind-kind wait --for=condition=Established crd canaries.flagger.app',
  resource_deps=['flagger'], labels='flagger')


# Install local k6-loadtester
docker_build('ghcr.io/grafana/flagger-k6-webhook:development', '.')
k8s_yaml(secret_from_dict('k6-loadtester', inputs={
    'KUBERNETES_CLIENT': 'in-cluster',
    'K6_LOG_FORMAT': 'json',
    'LOG_LEVEL': 'debug'
}))
yaml = helm('./charts/k6-loadtester', set=[
    'webhook.vars.KUBERNETES_CLIENT=in-cluster',
    'webhook.vars.K6_LOG_FORMAT=json',
    'image.tag=development',
    'logLevel=debug',
])
k8s_yaml(yaml)
k8s_resource('chart-k6-loadtester', labels='flagger')

# Now start a dev workload and let flagger create a route for it:
if os.path.exists('dev-workload.yml'):
    k8s_yaml('dev-workload.yml')
    k8s_resource('my-app', new_name='workload', objects=['my-app:Canary:default'], resource_deps=['flagger-crds-ready'], labels='workload')

