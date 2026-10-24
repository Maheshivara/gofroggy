package display

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const deviceEnv = "/tmp/tfdevice.env"

func getDriverPath() (string, error) {
	if path := os.Getenv("TF_DRIVER"); path != "" {
		return path, nil
	}

	f, err := os.Open(deviceEnv)
	if err != nil {
		return "", fmt.Errorf("open device config: %w", err)
	}
	defer func() {
		err = f.Close()
	}()

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "TF_DRIVER=") {
			path := strings.TrimSpace(
				strings.TrimPrefix(line, "TF_DRIVER="),
			)

			if path == "" {
				return "", fmt.Errorf("TF_DRIVER is empty")
			}

			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("driver %s: %w", path, err)
			}

			return path, err
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("TF_DRIVER not found in %s", deviceEnv)
}
