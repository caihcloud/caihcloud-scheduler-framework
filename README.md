# caihcloud-scheduler-framework

This repo is a scheduler using kubernetes scheduler framework.

This plugin relies on [caihcloud-node-annotator](https://github.com/caihcloud/caihcloud-node-annotator/tree/master) which regularly pulls real-time node load metrics, filtering and scoring during scheduling according to node load metrics.

## Deploy
```bash
$ kubectl apply -f deploy/scheduler-config.yaml
$ kubectl apply -f deploy/caihcloud-scheduler.yaml
```

## Test
```bash
$ kubectl apply -f deploy/test-scheduler.yaml
```

Then watch caihcloud-scheduler pod logs.

# Dependency 

+ k8s: 1.17.2
+ this plugin relies on [caihcloud-node-annotator](https://github.com/caihcloud/caihcloud-node-annotator/tree/master)
