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
local goStep(name, commands) = step(name, commands, image='golang:1.17-alpine');
local dockerStep(name, commands) = step(name, [
  'echo $CR_PAT | docker login ghcr.io -u USERNAME --password-stdin',
  // print length of token
  'echo $CR_PAT | wc -c',
], image='docker') {
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

local trigger(events) = {
  trigger: {
    event: {
      include: events,
    },
  },
};

[
  pipeline('docker') {
    steps: [
      dockerStep('push latest', ['make push-latest']),
    ],
  }
  + trigger(['pull_request']),

  github_secret,
]
