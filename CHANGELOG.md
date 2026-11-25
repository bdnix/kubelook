# Changelog

## [0.1.0-rc.2](https://github.com/bdnix/kubelook/compare/v0.1.0-rc.1...v0.1.0-rc.2) (2026-11-25)


### Bug Fixes

* **execs:** closing a shell while its first attempt starts no longer hangs ([062c8f1](https://github.com/bdnix/kubelook/commit/062c8f1a8c53b4cfd208b274fba77276a1583647))
* **watch:** a 401 from the API server is retried with a fresh client instead of failing the watch for good ([9909ff1](https://github.com/bdnix/kubelook/commit/9909ff184e48bfd30ce385fe15d71fca7a087872))
