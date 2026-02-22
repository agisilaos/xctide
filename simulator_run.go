package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type simDeviceInfo struct {
	Name string
	OS   string
}

type buildSettings struct {
	TargetBuildDir string
	WrapperName    string
	BundleID       string
}

func destinationSummary(destination string) (kind string, name string, osVersion string) {
	kind = "Destination"
	if strings.Contains(destination, "iOS Simulator") {
		kind = "Simulator"
	}
	if strings.Contains(destination, "platform=iOS") && !strings.Contains(destination, "Simulator") {
		kind = "Device"
	}

	for _, part := range strings.Split(destination, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "name=") {
			name = strings.TrimPrefix(part, "name=")
		}
		if strings.HasPrefix(part, "id=") {
			udid := strings.TrimPrefix(part, "id=")
			info := simulatorInfoForUDID(udid)
			if info.Name != "" {
				name = info.Name
			}
			if info.OS != "" {
				osVersion = info.OS
			}
		}
	}
	return kind, name, osVersion
}

func simulatorInfoForUDID(udid string) simDeviceInfo {
	if udid == "" {
		return simDeviceInfo{}
	}
	cmd := exec.Command("xcrun", "simctl", "list", "devices", "--json")
	out, err := cmd.Output()
	if err != nil {
		return simDeviceInfo{}
	}
	var payload struct {
		Devices map[string][]struct {
			UDID string `json:"udid"`
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return simDeviceInfo{}
	}
	for runtime, devices := range payload.Devices {
		for _, device := range devices {
			if strings.EqualFold(device.UDID, udid) {
				os := strings.TrimPrefix(runtime, "com.apple.CoreSimulator.SimRuntime.iOS-")
				os = strings.ReplaceAll(os, "-", ".")
				return simDeviceInfo{Name: device.Name, OS: os}
			}
		}
	}
	return simDeviceInfo{}
}

func destinationUDID(destination string) string {
	for _, part := range strings.Split(destination, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "id=") {
			return strings.TrimPrefix(part, "id=")
		}
	}
	return ""
}

func runAppOnSimulator(cfg buildConfig) ([]timedItem, error) {
	udid := destinationUDID(cfg.destination)
	if udid == "" {
		return nil, errors.New("run mode requires simulator destination with id=<UDID>")
	}
	info := simulatorInfoForUDID(udid)
	if info.Name == "" {
		return nil, fmt.Errorf("destination id %s is not a simulator device", udid)
	}
	settings, err := readBuildSettings(cfg)
	if err != nil {
		return nil, err
	}
	appPath := filepath.Join(settings.TargetBuildDir, settings.WrapperName)
	rows := make([]timedItem, 0, 3)
	duration, err := runTimedCommand("xcrun", "simctl", "boot", udid)
	if err == nil {
		rows = append(rows, timedItem{name: "Launch simulator", duration: duration})
	}
	if _, err := runTimedCommand("xcrun", "simctl", "bootstatus", udid, "-b"); err != nil {
		return rows, err
	}
	duration, err = runTimedCommand("xcrun", "simctl", "install", udid, appPath)
	if err != nil {
		return rows, err
	}
	rows = append(rows, timedItem{name: "Install iOS app", duration: duration})
	duration, err = runTimedCommand("xcrun", "simctl", "launch", udid, settings.BundleID)
	if err != nil {
		return rows, err
	}
	rows = append(rows, timedItem{name: "Launch iOS app", duration: duration})
	return rows, nil
}

func runTimedCommand(name string, args ...string) (time.Duration, error) {
	start := time.Now()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("Unable to boot device in current state: Booted")) {
			return time.Since(start), nil
		}
		return time.Since(start), fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return time.Since(start), nil
}

func readBuildSettings(cfg buildConfig) (buildSettings, error) {
	args := buildArgs(cfg)
	filtered := make([]string, 0, len(args)+1)
	for _, arg := range args {
		switch arg {
		case "build", "clean", "test", "archive", "analyze":
			continue
		default:
			filtered = append(filtered, arg)
		}
	}
	filtered = append(filtered, "-showBuildSettings")
	cmd := exec.Command("xcodebuild", filtered...)
	out, err := cmd.Output()
	if err != nil {
		return buildSettings{}, err
	}
	settings := buildSettings{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "TARGET_BUILD_DIR = ") {
			settings.TargetBuildDir = strings.TrimPrefix(line, "TARGET_BUILD_DIR = ")
		}
		if strings.HasPrefix(line, "WRAPPER_NAME = ") {
			settings.WrapperName = strings.TrimPrefix(line, "WRAPPER_NAME = ")
		}
		if strings.HasPrefix(line, "PRODUCT_BUNDLE_IDENTIFIER = ") {
			settings.BundleID = strings.TrimPrefix(line, "PRODUCT_BUNDLE_IDENTIFIER = ")
		}
	}
	if settings.TargetBuildDir == "" || settings.WrapperName == "" || settings.BundleID == "" {
		return buildSettings{}, errors.New("could not determine app settings from build settings")
	}
	return settings, nil
}
