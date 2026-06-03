// therm-pro-scan: a BLE discovery helper for the ThermoPro firmware.
//
// Scans for nearby BLE devices, picks the first one whose advertised name
// matches a substring (default: "thermo", case-insensitive), connects,
// enumerates services and characteristics, and prints the BLE name, primary
// service UUID, write characteristic UUID, and notify characteristic UUID as
// shell-friendly env-var lines that `make esp32-config` consumes.
//
// Usage:
//
//	therm-pro-scan                       # scan & pick first "thermo*" match
//	therm-pro-scan -name TP25            # different name substring
//	therm-pro-scan -address AA:BB:...    # bypass name filter
//	therm-pro-scan -timeout 20s          # scan timeout
//	therm-pro-scan -output .env.esp32-ble  # also write env vars to a file
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

func main() {
	nameFilter := flag.String("name", "thermo", "case-insensitive substring to match against advertised local name")
	addrFilter := flag.String("address", "", "exact device address to connect to (skips name filter)")
	timeout := flag.Duration("timeout", 15*time.Second, "scan timeout")
	listAll := flag.Bool("list", false, "list all matching devices and exit without connecting")
	diff := flag.Bool("diff", false, "two-pass diff scan: snapshot, prompt for power-cycle, rescan, show new devices")
	probe := flag.Bool("probe", false, "scan, filter known consumer-brand noise, then connect to each survivor and classify by service shape")
	noFilter := flag.Bool("no-filter", false, "in -probe mode, skip the consumer-brand noise filter")
	output := flag.String("output", "", "write env vars to this file in addition to stdout")
	flag.Parse()

	if *diff {
		if err := runDiff(*timeout, *output); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if *probe {
		if err := runProbe(*timeout, *noFilter, *output); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*nameFilter, *addrFilter, *timeout, *listAll, *output); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(nameFilter, addrFilter string, timeout time.Duration, listAll bool, output string) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("enable adapter: %w", err)
	}

	match, err := scan(adapter, nameFilter, addrFilter, timeout, listAll)
	if err != nil {
		return err
	}
	if listAll {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Connecting to %q [%s]...\n", match.LocalName(), match.Address.String())
	return connectAndEmit(adapter, match, output)
}

type verdict struct {
	res        bluetooth.ScanResult
	confidence string // "HIGH", "MEDIUM", "LOW", "ERROR"
	svc        bluetooth.UUID
	writeChar  bluetooth.UUID
	notifyChar bluetooth.UUID
	note       string
}

// tp25ServiceUUID is the vendor service used by the original ThermoPro TP25
// firmware. A device that advertises (or exposes after connect) this UUID is a
// confident match.
var tp25ServiceUUID = mustParseUUID("1086fff0-3343-4817-8bb2-b32206336ce8")

func mustParseUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// runProbe scans, drops obvious consumer-brand noise, then connects to every
// surviving device and classifies it by service shape. Devices that expose the
// TP25 service UUID are reported as HIGH-confidence matches; devices that have
// a custom (128-bit) service with both a notifiable and a non-notifiable
// characteristic are reported as MEDIUM.
//
// Before scanning, runProbe tries a fast path: if -output names an existing
// .ble-config that records an ESP32_BLE_ADDRESS, attempt a direct connect to
// that address. On a HIGH verdict we re-emit and skip the scan entirely. The
// fast path is what lets a successful scan be reused across subsequent
// invocations without re-discovering the unit each time.
//
// During the full scan, the candidate loop short-circuits on the first HIGH
// verdict so we don't waste time probing the rest of the noise once a
// confident match is in hand.
func runProbe(timeout time.Duration, noFilter bool, output string) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("enable adapter: %w", err)
	}

	// Fast path: try the cached address from a previous run.
	if output != "" {
		if cached := readCachedField(output, "ESP32_BLE_ADDRESS"); cached != "" {
			cachedName := readCachedField(output, "ESP32_BLE_NAME")
			fmt.Fprintf(os.Stderr, "Fast path: trying cached address %s %q...\n", cached, cachedName)
			if v, ok := tryCachedAddress(adapter, cached, cachedName); ok {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", v.confidence, v.note)
				fmt.Fprintf(os.Stderr, "\nFast-path hit: skipping scan.\n")
				return emitEnv(cachedName, cached, v.svc, v.writeChar, v.notifyChar, output)
			}
			fmt.Fprintln(os.Stderr, "  fast path miss; falling through to full scan.")
		}
	}

	fmt.Fprintf(os.Stderr, "Scanning for %s...\n", timeout)
	all, err := snapshot(adapter, timeout, "probe")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Saw %d device(s).\n", len(all))

	type cand struct {
		res    bluetooth.ScanResult
		reason string
	}
	var candidates []cand
	for _, r := range all {
		if !noFilter {
			if skip, why := looksLikeNoise(r); skip {
				fmt.Fprintf(os.Stderr, "  skip %s rssi=%d name=%q (%s)\n", r.Address.String(), r.RSSI, r.LocalName(), why)
				continue
			}
		}
		candidates = append(candidates, cand{r, ""})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].res.RSSI > candidates[j].res.RSSI })

	fmt.Fprintf(os.Stderr, "\n%d candidate(s) after filter:\n", len(candidates))

	var verdicts []verdict

	for _, c := range candidates {
		r := c.res
		fmt.Fprintf(os.Stderr, "  probing %s rssi=%d name=%q...\n", r.Address.String(), r.RSSI, r.LocalName())
		v := probeOne(adapter, r)
		verdicts = append(verdicts, v)
		fmt.Fprintf(os.Stderr, "    %s: %s\n", v.confidence, v.note)
		if v.confidence == "HIGH" {
			fmt.Fprintln(os.Stderr, "  short-circuit: HIGH-confidence TP25 match, skipping remaining candidates")
			break
		}
	}

	var high, medium []verdict
	for _, v := range verdicts {
		switch v.confidence {
		case "HIGH":
			high = append(high, v)
		case "MEDIUM":
			medium = append(medium, v)
		}
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Summary:")
	for _, v := range verdicts {
		fmt.Fprintf(os.Stderr, "  %-6s %s %q\n", v.confidence, v.res.Address.String(), v.res.LocalName())
	}

	var pick *verdict
	switch {
	case len(high) == 1:
		pick = &high[0]
	case len(high) == 0 && len(medium) == 1:
		pick = &medium[0]
	}

	if pick == nil {
		fmt.Fprintln(os.Stderr, "")
		if len(high)+len(medium) == 0 {
			fmt.Fprintln(os.Stderr, "No HIGH or MEDIUM matches. The unit may not be advertising, or may use a service shape we don't recognise. Try -no-filter to widen the candidate set.")
			return errors.New("no match")
		}
		fmt.Fprintln(os.Stderr, "Multiple candidates — re-run with -address <UUID> to pick one explicitly.")
		return errors.New("ambiguous")
	}

	fmt.Fprintf(os.Stderr, "\nSelected %s match: %s %q\n", pick.confidence, pick.res.Address.String(), pick.res.LocalName())
	return emitEnv(pick.res.LocalName(), pick.res.Address.String(), pick.svc, pick.writeChar, pick.notifyChar, output)
}

func probeOne(adapter *bluetooth.Adapter, r bluetooth.ScanResult) verdict {
	v := verdict{res: r}
	// 5-second connection timeout (units of 0.625ms).
	dev, err := adapter.Connect(r.Address, bluetooth.ConnectionParams{ConnectionTimeout: 8000})
	if err != nil {
		v.confidence = "ERROR"
		v.note = "connect failed: " + err.Error()
		return v
	}
	defer dev.Disconnect()

	conf, note, svc, writeChar, notifyChar := probeConnected(dev)
	v.confidence, v.note, v.svc, v.writeChar, v.notifyChar = conf, note, svc, writeChar, notifyChar
	return v
}

// tryCachedAddress attempts to connect directly to a previously-discovered
// address (CoreBluetooth UUID on macOS) and probe its services. Returns the
// verdict and ok=true if a HIGH match is found; ok=false otherwise so the
// caller can fall back to a full scan.
func tryCachedAddress(adapter *bluetooth.Adapter, address, name string) (verdict, bool) {
	var v verdict
	addrUUID, err := bluetooth.ParseUUID(address)
	if err != nil {
		v.confidence, v.note = "ERROR", "parse cached address: "+err.Error()
		return v, false
	}
	addr := bluetooth.Address{UUID: addrUUID}

	dev, err := adapter.Connect(addr, bluetooth.ConnectionParams{ConnectionTimeout: 8000})
	if err != nil {
		v.confidence, v.note = "ERROR", "cached connect failed: "+err.Error()
		return v, false
	}
	defer dev.Disconnect()

	conf, note, svc, writeChar, notifyChar := probeConnected(dev)
	v.confidence, v.note, v.svc, v.writeChar, v.notifyChar = conf, note, svc, writeChar, notifyChar
	return v, conf == "HIGH"
}

// probeConnected runs the service-shape classification against an already-
// connected Device. It is the shared core between the full-scan probe and the
// fast-path cache hit. Returns (confidence, note, service, writeChar, notifyChar).
func probeConnected(dev bluetooth.Device) (string, string, bluetooth.UUID, bluetooth.UUID, bluetooth.UUID) {
	var zeroUUID bluetooth.UUID
	services, err := dev.DiscoverServices(nil)
	if err != nil {
		return "ERROR", "discover services failed: " + err.Error(), zeroUUID, zeroUUID, zeroUUID
	}

	// First pass: TP25 service UUID present?
	for _, s := range services {
		if s.UUID() == tp25ServiceUUID {
			chars, cerr := s.DiscoverCharacteristics(nil)
			if cerr != nil || len(chars) < 2 {
				return "HIGH", "TP25 service present (could not enumerate characteristics)", s.UUID(), zeroUUID, zeroUUID
			}
			svc, writeChar, notifyChar, ok := classifyChars(s.UUID(), chars)
			if !ok {
				return "HIGH", "TP25 service present but characteristic shape unexpected", s.UUID(), zeroUUID, zeroUUID
			}
			return "HIGH", "TP25 service UUID matched", svc, writeChar, notifyChar
		}
	}

	// Second pass: any custom service with notify+write?
	for _, s := range services {
		if isStandardService(s.UUID()) {
			continue
		}
		chars, cerr := s.DiscoverCharacteristics(nil)
		if cerr != nil {
			continue
		}
		svc, writeChar, notifyChar, ok := classifyChars(s.UUID(), chars)
		if ok {
			return "MEDIUM", "custom service " + svc.String() + " with notify+write pair", svc, writeChar, notifyChar
		}
	}
	return "LOW", fmt.Sprintf("connected (%d services) but no notify+write custom service", len(services)), zeroUUID, zeroUUID, zeroUUID
}

// readCachedField extracts a single shell-style assignment (KEY=VALUE) from a
// .ble-config file. Lines that don't match or are blank/comments are ignored.
// Values may be optionally double-quoted; quotes are stripped.
func readCachedField(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	prefix := key + "="
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		val := strings.TrimPrefix(line, prefix)
		val = strings.Trim(val, `"`)
		return val
	}
	return ""
}

// classifyChars empirically probes each characteristic for notifiability and
// returns (service, writeChar, notifyChar, ok). ok is false if no notifiable
// char is found or no separate write target exists.
func classifyChars(svcUUID bluetooth.UUID, chars []bluetooth.DeviceCharacteristic) (bluetooth.UUID, bluetooth.UUID, bluetooth.UUID, bool) {
	notifyIdx := -1
	for i, c := range chars {
		if tryEnableNotifications(c) {
			notifyIdx = i
			break
		}
	}
	if notifyIdx == -1 || len(chars) < 2 {
		return bluetooth.UUID{}, bluetooth.UUID{}, bluetooth.UUID{}, false
	}
	for i := range chars {
		if i == notifyIdx {
			continue
		}
		return svcUUID, chars[i].UUID(), chars[notifyIdx].UUID(), true
	}
	return bluetooth.UUID{}, bluetooth.UUID{}, bluetooth.UUID{}, false
}

// looksLikeNoise filters out devices that are almost certainly not a Therm-Pro:
// Apple-only manufacturer data (iPhones, AirPods, Watches, Macs, AirTags),
// branded consumer hardware we've seen in earlier scans, and devices whose
// names start with well-known consumer prefixes.
func looksLikeNoise(r bluetooth.ScanResult) (bool, string) {
	name := strings.ToLower(strings.TrimSpace(r.LocalName()))
	mfg := r.ManufacturerData()

	// Apple-only manufacturer data with no name and no services: almost always
	// an iPhone/Mac/AirPods/AirTag advertising nearby presence info.
	if name == "" && len(r.ServiceUUIDs()) == 0 && allAppleMfg(mfg) {
		return true, "apple-only mfg, no name/services"
	}

	for _, prefix := range []string{
		"govee_",
		"samsung ",
		"galaxy ",
		"lg ",
		"core200s",
		"core300s",
		"core400s",
		"levoit",
		"airpods",
		"airpod",
		"sony ",
		"bose ",
		"jbl ",
		"echo ",
		"tile ",
		"airtag",
		"whoop",
		"ledble-",
		"wbb",
		"wb",
	} {
		if strings.HasPrefix(name, prefix) {
			return true, "name prefix " + prefix
		}
	}

	// Samsung Q-series/Frame TVs use the "0x0075" company ID heavily and have
	// LG/Samsung-style names. Already caught above.

	return false, ""
}

func allAppleMfg(elems []bluetooth.ManufacturerDataElement) bool {
	if len(elems) == 0 {
		return false
	}
	for _, e := range elems {
		if e.CompanyID != 0x004c {
			return false
		}
	}
	return true
}

// runDiff scans the environment twice, prompting between passes so the user
// can power-cycle the target. Any address that appears only in the second pass
// is reported as a candidate; if exactly one shows up we attempt to connect
// and emit env vars like the single-shot mode.
func runDiff(timeout time.Duration, output string) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("enable adapter: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Diff mode:")
	fmt.Fprintln(os.Stderr, "  1) Make sure the Therm-Pro unit is OFF (and iOS Bluetooth is OFF too).")
	fmt.Fprintln(os.Stderr, "  2) Press Enter to take the baseline scan.")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return err
	}

	before, err := snapshot(adapter, timeout, "baseline")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Baseline: %d device(s) seen.\n", len(before))

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  3) Power ON the Therm-Pro unit now.")
	fmt.Fprintln(os.Stderr, "  4) Press Enter when it's powered up.")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return err
	}

	after, err := snapshot(adapter, timeout, "after")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Second pass: %d device(s) seen.\n", len(after))

	type cand struct {
		addr string
		res  bluetooth.ScanResult
	}
	var newOnes []cand
	for addr, r := range after {
		if _, seen := before[addr]; !seen {
			newOnes = append(newOnes, cand{addr, r})
		}
	}
	sort.Slice(newOnes, func(i, j int) bool { return newOnes[i].res.RSSI > newOnes[j].res.RSSI })

	if len(newOnes) == 0 {
		return errors.New("no new devices appeared between scans. The unit may not be advertising, may already be bonded to another central, or its scan window may be too narrow")
	}

	fmt.Fprintf(os.Stderr, "\n%d new device(s) appeared:\n", len(newOnes))
	for _, c := range newOnes {
		r := c.res
		fmt.Fprintf(os.Stderr, "  %s rssi=%d name=%q%s%s\n",
			r.Address.String(), r.RSSI, r.LocalName(),
			formatServiceUUIDs(r.ServiceUUIDs()),
			formatManufacturer(r.ManufacturerData()),
		)
	}

	if len(newOnes) > 1 {
		fmt.Fprintln(os.Stderr, "\nMultiple candidates — re-run with -address <UUID> to connect to a specific one.")
		return nil
	}

	target := newOnes[0].res
	fmt.Fprintf(os.Stderr, "\nConnecting to %s...\n", target.Address.String())
	return connectAndEmit(adapter, target, output)
}

func snapshot(adapter *bluetooth.Adapter, timeout time.Duration, label string) (map[string]bluetooth.ScanResult, error) {
	results := map[string]bluetooth.ScanResult{}
	var (
		mu        sync.Mutex
		done      = make(chan struct{})
		closeOnce sync.Once
	)
	go func() {
		time.Sleep(timeout)
		closeOnce.Do(func() {
			_ = adapter.StopScan()
			close(done)
		})
	}()

	fmt.Fprintf(os.Stderr, "Scanning (%s, %s)...\n", label, timeout)
	err := adapter.Scan(func(_ *bluetooth.Adapter, r bluetooth.ScanResult) {
		mu.Lock()
		defer mu.Unlock()
		results[strings.ToLower(r.Address.String())] = r
	})
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	<-done
	return results, nil
}

func connectAndEmit(adapter *bluetooth.Adapter, target bluetooth.ScanResult, output string) error {
	dev, err := adapter.Connect(target.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer dev.Disconnect()

	svc, writeChar, notifyChar, err := pickCharacteristics(dev)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	return emitEnv(target.LocalName(), target.Address.String(), svc, writeChar, notifyChar, output)
}

// emitEnv writes the discovered BLE identifiers as shell-sourceable
// KEY=VALUE lines to stderr (via stdout for the values block) and optionally
// to `output`. ESP32_BLE_ADDRESS is the macOS CoreBluetooth UUID for the
// device; it isn't consumed by the firmware but lets a subsequent scan skip
// straight to a direct connect.
func emitEnv(name, address string, svc, writeChar, notifyChar bluetooth.UUID, output string) error {
	keys := []string{
		"ESP32_BLE_NAME",
		"ESP32_BLE_ADDRESS",
		"ESP32_BLE_SERVICE_UUID",
		"ESP32_BLE_WRITE_CHAR_UUID",
		"ESP32_BLE_NOTIFY_CHAR_UUID",
	}
	values := map[string]string{
		"ESP32_BLE_NAME":             name,
		"ESP32_BLE_ADDRESS":          address,
		"ESP32_BLE_SERVICE_UUID":     svc.String(),
		"ESP32_BLE_WRITE_CHAR_UUID":  writeChar.String(),
		"ESP32_BLE_NOTIFY_CHAR_UUID": notifyChar.String(),
	}
	var lines strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&lines, "%s=%s\n", k, values[k])
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Discovered:")
	fmt.Print(lines.String())

	if output != "" {
		if err := os.WriteFile(output, []byte(lines.String()), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", output, err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", output)
	}
	return nil
}

func scan(adapter *bluetooth.Adapter, nameFilter, addrFilter string, timeout time.Duration, listAll bool) (bluetooth.ScanResult, error) {
	nameFilter = strings.ToLower(strings.TrimSpace(nameFilter))
	addrFilter = strings.ToLower(strings.TrimSpace(addrFilter))

	var (
		mu       sync.Mutex
		seen     = map[string]bool{}
		matched  []bluetooth.ScanResult
		done     = make(chan struct{})
		closeOnce sync.Once
	)

	go func() {
		time.Sleep(timeout)
		closeOnce.Do(func() {
			_ = adapter.StopScan()
			close(done)
		})
	}()

	fmt.Fprintf(os.Stderr, "Scanning for %s (timeout %s)...\n", describeFilter(nameFilter, addrFilter), timeout)

	err := adapter.Scan(func(_ *bluetooth.Adapter, result bluetooth.ScanResult) {
		addr := strings.ToLower(result.Address.String())
		name := result.LocalName()

		mu.Lock()
		if seen[addr] {
			mu.Unlock()
			return
		}
		seen[addr] = true
		mu.Unlock()

		if addrFilter != "" {
			if addr != addrFilter {
				return
			}
		} else if nameFilter != "" {
			if !strings.Contains(strings.ToLower(name), nameFilter) {
				return
			}
		}

		fmt.Fprintf(os.Stderr, "  found: name=%q address=%s rssi=%d%s%s\n",
			name, result.Address.String(), result.RSSI,
			formatServiceUUIDs(result.ServiceUUIDs()),
			formatManufacturer(result.ManufacturerData()),
		)
		mu.Lock()
		matched = append(matched, result)
		first := len(matched) == 1
		mu.Unlock()

		if first && !listAll {
			closeOnce.Do(func() {
				_ = adapter.StopScan()
				close(done)
			})
		}
	})
	if err != nil {
		return bluetooth.ScanResult{}, fmt.Errorf("scan: %w", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(matched) == 0 {
		return bluetooth.ScanResult{}, errors.New("no matching device found")
	}
	return matched[0], nil
}

func describeFilter(name, addr string) string {
	switch {
	case addr != "":
		return fmt.Sprintf("address=%s", addr)
	case name != "":
		return fmt.Sprintf("name substring %q", name)
	default:
		return "all devices"
	}
}

// pickCharacteristics walks every service on the connected device and looks
// for the first one that has at least one notifiable characteristic plus at
// least one other characteristic to use as the write target. Notifiability is
// detected empirically by attempting EnableNotifications — there is no
// property accessor on the connected-side characteristic on darwin.
func pickCharacteristics(dev bluetooth.Device) (svc, writeChar, notifyChar bluetooth.UUID, err error) {
	services, err := dev.DiscoverServices(nil)
	if err != nil {
		return svc, writeChar, notifyChar, fmt.Errorf("discover services: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Discovered %d service(s)\n", len(services))

	for _, s := range services {
		uuid := s.UUID()
		// Skip standard services that don't carry vendor protocols.
		if isStandardService(uuid) {
			continue
		}

		chars, cerr := s.DiscoverCharacteristics(nil)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "  service %s: discover chars failed: %v\n", uuid, cerr)
			continue
		}
		fmt.Fprintf(os.Stderr, "  service %s (%d characteristics)\n", uuid, len(chars))

		var notifyIdx = -1
		for i, c := range chars {
			cu := c.UUID()
			supportsNotify := tryEnableNotifications(c)
			fmt.Fprintf(os.Stderr, "    char %s notify=%v\n", cu, supportsNotify)
			if supportsNotify && notifyIdx == -1 {
				notifyIdx = i
			}
		}

		if notifyIdx == -1 || len(chars) < 2 {
			continue
		}

		// Pick the first non-notify characteristic as the write target.
		writeIdx := -1
		for i := range chars {
			if i == notifyIdx {
				continue
			}
			writeIdx = i
			break
		}
		if writeIdx == -1 {
			continue
		}

		return uuid, chars[writeIdx].UUID(), chars[notifyIdx].UUID(), nil
	}
	return svc, writeChar, notifyChar, errors.New("no service with notify+write characteristics found")
}

func tryEnableNotifications(c bluetooth.DeviceCharacteristic) bool {
	cb := func(_ []byte) {}
	if err := c.EnableNotifications(cb); err != nil {
		return false
	}
	_ = c.EnableNotifications(nil)
	return true
}

func formatServiceUUIDs(uuids []bluetooth.UUID) string {
	if len(uuids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(uuids))
	for _, u := range uuids {
		parts = append(parts, u.String())
	}
	return " services=[" + strings.Join(parts, ",") + "]"
}

func formatManufacturer(elems []bluetooth.ManufacturerDataElement) string {
	if len(elems) == 0 {
		return ""
	}
	parts := make([]string, 0, len(elems))
	for _, e := range elems {
		hexStr := ""
		const maxBytes = 16
		n := len(e.Data)
		if n > maxBytes {
			n = maxBytes
		}
		for i := 0; i < n; i++ {
			hexStr += fmt.Sprintf("%02x", e.Data[i])
		}
		if len(e.Data) > maxBytes {
			hexStr += "..."
		}
		parts = append(parts, fmt.Sprintf("0x%04x:%s", e.CompanyID, hexStr))
	}
	return " mfg=[" + strings.Join(parts, ",") + "]"
}

func isStandardService(u bluetooth.UUID) bool {
	if !u.Is16Bit() {
		return false
	}
	// Generic Access (0x1800), Generic Attribute (0x1801), Device Information
	// (0x180A), Battery Service (0x180F) are the common standard services
	// that show up alongside vendor protocols.
	switch u {
	case bluetooth.ServiceUUIDGenericAccess,
		bluetooth.ServiceUUIDGenericAttribute,
		bluetooth.ServiceUUIDDeviceInformation,
		bluetooth.ServiceUUIDBattery:
		return true
	}
	return false
}
