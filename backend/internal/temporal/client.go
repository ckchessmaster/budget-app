package temporal

import (
	"log/slog"
	"os"

	"go.temporal.io/sdk/client"
)

const TaskQueue = "budget-sync-queue"

func AddressFromEnv() string {
	if addr := os.Getenv("TEMPORAL_ADDRESS"); addr != "" {
		return addr
	}
	return "localhost:7233"
}

func NamespaceFromEnv() string {
	if ns := os.Getenv("TEMPORAL_NAMESPACE"); ns != "" {
		return ns
	}
	return "default"
}

func NewClient(logger *slog.Logger) (client.Client, error) {
	addr := AddressFromEnv()
	ns := NamespaceFromEnv()

	c, err := client.Dial(client.Options{
		HostPort:  addr,
		Namespace: ns,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}
