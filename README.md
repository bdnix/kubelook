# Kubelook

A browser-based Kubernetes console. Sign in with a Vault token or an uploaded kubeconfig and operate every cluster that credential reaches, with no desktop install.

> **Under construction.** Built so far: sign-in, Vault and kubeconfig clusters, live resource lists, logs, shells, YAML editing, workload and node actions and the pod file browser. They are covered by unit tests and have not yet been run against a real cluster or Vault; that is the next step of the [implementation plan](PLAN.md). Not built yet: Helm, Flux, the ecosystem modules, the Helm chart and a release.

## Spec

Goals, features, architecture, security model and delivery plan are in [`docs/spec.md`](docs/spec.md). Feedback on the spec is the most useful contribution right now.

## License

[MIT](LICENSE) © 2026 bdnix
