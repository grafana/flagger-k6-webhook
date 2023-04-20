local pipeline(name) = {
  kind: 'pipeline',
  name: name,
  volumes: [
    {
      name: 'docker',
      host: {
        path: '/var/run/docker.sock',
      },
    },
  ],
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
local goStep(name, commands) = step(name, commands, image='golang:1.20-alpine');
local dockerStep(name, commands) = step(name, [
  'apk add git make',
  'echo $CR_PAT | docker login ghcr.io -u USERNAME --password-stdin',
] + commands, image='docker') {
  environment: {
    CR_PAT: { from_secret: github_secret.name },
  },
  volumes: [
    {
      name: 'docker',
      path: '/var/run/docker.sock',
    },
  ],
};
local fetchTagsStep = step('fetch tags', commands=['git fetch --tags'], image='alpine/git');

local trigger(events=[], branches=[]) = {
  trigger:
    {
      [if events != [] then 'event']: {
        include: events,
      },
      [if branches != [] then 'branch']: {
        include: branches,
      },
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
  + trigger(events=['pull_request']),

  pipeline('docker') {
    steps: [
      fetchTagsStep,
      dockerStep('build', ['make build']),
      dockerStep('push tag', ['make push']) { when: {
        event: ['tag'],
      } },
      dockerStep('push latest', ['make push-latest']) { when: {
        branch: ['main'],
        event: ['push'],
      } },
    ],
  }
  + trigger(events=['pull_request', 'push', 'tag'], branches=['main']),

  github_secret,
]
