local pipeline(name) = {
  kind: 'pipeline',
  name: name,
};

local secret(name, vault_path, vault_key) = {
  kind: 'secret',
  name: name,
  get: {
    path: vault_path,
    name: vault_key,
  },
};
local github_secret = secret('github_token', 'infra/data/ci/github/grafanabot', 'pat');

local step(name, commands, image) = {
  name: name,
  image: image,
  commands: commands,
};
local goStep(name, commands) = step(name, commands, image='golang:1.17-alpine');
local dockerStep(name, commands) = step(name, [
  'apk add make',
  'echo $CR_PAT | docker login ghcr.io -u USERNAME --password-stdin',
] + commands, image='docker') { environment: { CR_PAT: { from_secret: github_secret.name } } };

local trigger(branches, events) = {
  branch: branches,
  event: {
    include: events,
  },
};

[
  pipeline('test') {
    environment: {
      GOARCH: 'amd64',
      GOOS: 'linux',
      CGO_ENABLED: '0',
    },
    steps: [
      goStep('build', ['go build ./...']),
      goStep('test', ['go test ./...']),
      step('lint', ['golangci-lint run'], image='golangci/golangci-lint'),
    ],
  }
  + trigger(['main'], ['pull_request', 'push']),

  pipeline('docker') {
    steps: [
      dockerStep('build', ['make build']),
      dockerStep('push tag', ['make push']) { when: {
        branch: ['main'],
        event: ['tag'],
      } },
      dockerStep('push latest', ['make push-latest']) { when: {
        branch: ['main'],
        event: ['push'],
      } },
    ],
  }
  + trigger(['main'], ['pull_request', 'push', 'tag']),

  github_secret,
]
