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

[
  pipeline('test') {
    steps: [
      {
        name: 'build',
        commands: ['go build ./...'],
        image: 'golang:1.17-alpine',
      },
      {
        name: 'test',
        commands: ['go test ./...'],
        image: 'golang:1.17-alpine',
      },
      {
        name: 'lint',
        commands: ['golangci-lint run'],
        image: 'golangci/golangci-lint',
      },
    ],
    trigger: {
      branch: ['main'],
      event: {
        include: ['pull_request', 'push'],
      },
    },
  },
  github_secret,
]
