package app

// Version is the application version. It is injected at build time via
// -ldflags "-X ic9700-remote-io/internal/app.Version=x.y.z". The fallback
// "dev" is used for local/development builds and is treated as older than
// every released version, so an update is always offered in development.
var Version = "dev"
