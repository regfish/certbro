// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package app implements the certbro CLI and its certificate management workflows.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/regfish/certbro/internal/api"
	"github.com/regfish/certbro/internal/config"
	certcrypto "github.com/regfish/certbro/internal/crypto"
	"github.com/regfish/certbro/internal/deploy"
	"github.com/regfish/certbro/internal/lock"
	"github.com/regfish/certbro/internal/systemd"
)

// App bundles build metadata and command handlers for the certbro CLI.
type App struct {
	Version   string
	Commit    string
	BuildDate string
}

type rootOptions struct {
	StateFile       string
	APIKey          string
	APIBaseURL      string
	CertificatesDir string
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// New constructs an App with the supplied build metadata.
func New(version, commit, buildDate string) *App {
	return &App{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}
}

// Run parses root flags, loads state, and dispatches the requested subcommand.
func (a *App) Run(ctx context.Context, args []string) error {
	root := flag.NewFlagSet("certbro", flag.ContinueOnError)
	root.SetOutput(os.Stderr)

	defaultStateFile, err := config.DefaultPath()
	if err != nil {
		return err
	}

	var opts rootOptions
	root.StringVar(&opts.StateFile, "state-file", defaultStateFile, "path to certbro state file")
	root.StringVar(&opts.APIKey, "api-key", "", "regfish API key")
	root.StringVar(&opts.APIBaseURL, "api-base-url", "", "override API base URL")
	root.StringVar(&opts.CertificatesDir, "certificates-dir", strings.TrimSpace(os.Getenv("CERTBRO_CERTIFICATES_DIR")), "root directory for certbro-managed certificate directories")
	root.Usage = func() {
		a.printRootUsage()
	}

	if err := root.Parse(args); err != nil {
		return err
	}

	rest := root.Args()
	if len(rest) == 0 {
		a.printRootUsage()
		return nil
	}

	store, err := config.Load(opts.StateFile)
	if err != nil {
		return err
	}

	switch rest[0] {
	case "help":
		a.printRootUsage()
		return nil
	case "version":
		fmt.Printf("certbro %s\n", a.Version)
		return nil
	case "configure":
		return a.runConfigure(ctx, rest[1:], opts.StateFile, store)
	case "install":
		return a.runInstall(rest[1:], opts, store)
	case "import":
		return a.runImport(ctx, rest[1:], opts, store)
	case "list":
		return a.runList(rest[1:], store)
	case "update":
		return a.runUpdate(rest[1:], opts, store)
	case "issue":
		return a.runIssue(ctx, rest[1:], opts, store)
	case "issue-pair":
		return a.runIssuePair(ctx, rest[1:], opts, store)
	case "renew":
		return a.runRenew(ctx, rest[1:], opts, store)
	default:
		return fmt.Errorf("unknown command %q", rest[0])
	}
}

// runConfigure stores API settings and verifies credentials before persisting them.
func (a *App) runConfigure(ctx context.Context, args []string, stateFile string, store *config.Store) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var apiKey string
	var apiBaseURL string
	fs.StringVar(&apiKey, "api-key", "", "regfish API key")
	fs.StringVar(&apiBaseURL, "api-base-url", "", "API base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(apiKey) == "" && strings.TrimSpace(apiBaseURL) == "" {
		fmt.Printf("state file: %s\n", stateFile)
		fmt.Printf("api base url: %s\n", firstNonEmpty(store.APIBaseURL, api.DefaultBaseURL))
		if store.APIKey != "" {
			if store.APIKeyValidatedAt != nil {
				fmt.Printf("api key: configured and verified at %s\n", store.APIKeyValidatedAt.UTC().Format(time.RFC3339))
			} else {
				fmt.Printf("api key: configured but not verified\n")
			}
		} else {
			fmt.Printf("api key: not configured\n")
		}
		return nil
	}

	candidate := *store
	if strings.TrimSpace(apiKey) != "" {
		candidate.APIKey = strings.TrimSpace(apiKey)
	}
	if strings.TrimSpace(apiBaseURL) != "" {
		candidate.APIBaseURL = strings.TrimSpace(apiBaseURL)
	}

	if strings.TrimSpace(apiKey) != "" || strings.TrimSpace(apiBaseURL) != "" {
		client, err := a.newClientFromStore(rootOptions{}, &candidate)
		if err != nil {
			return err
		}
		if err := client.ValidateCredentials(ctx); err != nil {
			return fmt.Errorf("validate API key: %w", err)
		}
		validatedAt := time.Now().UTC()
		candidate.APIKeyValidatedAt = &validatedAt
	}

	*store = candidate
	if err := config.Save(stateFile, store); err != nil {
		return err
	}

	fmt.Printf("updated %s\n", stateFile)
	return nil
}

// runList renders the managed certificate inventory in text or JSON form.
func (a *App) runList(args []string, store *config.Store) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "render managed certificates as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	managed := append([]config.ManagedCertificate(nil), store.ManagedCertificates...)
	sort.Slice(managed, func(i, j int) bool {
		return managed[i].Name < managed[j].Name
	})

	if asJSON {
		raw, err := json.MarshalIndent(managed, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}

	if len(managed) == 0 {
		fmt.Println("no managed certificates configured")
		return nil
	}

	for _, cert := range managed {
		fmt.Printf("%s\n", cert.Name)
		fmt.Printf("  certificate_id: %s\n", emptyFallback(cert.CertificateID, "-"))
		fmt.Printf("  common_name: %s\n", cert.CommonName)
		fmt.Printf("  dns_names: %s\n", strings.Join(certcrypto.NormalizeDNSNames("", cert.DNSNames), ", "))
		fmt.Printf("  webserver: %s\n", emptyFallback(cert.Webserver, "-"))
		if cert.WebserverConfig != "" {
			fmt.Printf("  webserver_config: %s\n", cert.WebserverConfig)
		}
		fmt.Printf("  key_type: %s\n", emptyFallback(cert.KeyType, config.DefaultKeyType))
		if strings.EqualFold(cert.KeyType, certcrypto.KeyTypeECDSA) {
			fmt.Printf("  ecdsa_curve: %s\n", emptyFallback(cert.ECDSACurve, config.DefaultECDSACurve))
		} else {
			fmt.Printf("  rsa_bits: %d\n", cert.RSABits)
		}
		fmt.Printf("  status: %s\n", emptyFallback(cert.Status, "-"))
		fmt.Printf("  valid_until: %s\n", formatOptionalTime(cert.ValidUntil))
		fmt.Printf("  contract_valid_until: %s\n", formatOptionalTime(cert.ContractValidUntil))
		fmt.Printf("  output_dir: %s\n", cert.OutputDir)
		fmt.Printf("  pending_action: %s\n", emptyFallback(cert.PendingAction, "-"))
	}
	return nil
}

// runUpdate adjusts mutable per-certificate settings in the local management state.
func (a *App) runUpdate(args []string, root rootOptions, store *config.Store) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var name string
	validityDays := -1
	fs.StringVar(&name, "name", "", "managed certificate name")
	fs.IntVar(&validityDays, "validity-days", validityDays, "requested certificate validity in days for future renewal orders")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}
	if err := config.ValidateValidityDaysAt(validityDays, time.Now().UTC()); err != nil {
		return err
	}

	sourceStore, err := a.storeForRenew(root, store)
	if err != nil {
		return err
	}
	managed, _ := sourceStore.FindManagedCertificate(name)
	if managed == nil {
		return fmt.Errorf("managed certificate %q not found", name)
	}

	updated := *managed
	updated.ValidityDays = validityDays
	updated.UpdatedAt = time.Now().UTC()

	if err := config.SaveManagedCertificate(updated.OutputDir, updated); err != nil {
		return err
	}
	store.UpsertManagedCertificate(updated)
	if err := config.Save(root.StateFile, store); err != nil {
		return err
	}

	fmt.Printf("%s: updated\n", updated.Name)
	fmt.Printf("certificate_id: %s\n", emptyFallback(updated.CertificateID, "-"))
	fmt.Printf("validity_days: %d\n", updated.ValidityDays)
	return nil
}

func (a *App) runIssue(ctx context.Context, args []string, root rootOptions, store *config.Store) error {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	defaultValidityDays := config.DefaultValidityDaysAt(time.Now().UTC())

	var name string
	var commonName string
	var dnsNames stringList
	var product string
	var outputDir string
	var installHook string
	var webserver string
	var webserverConfig string
	var orgID int
	var validityDays int
	var keyType string
	var rsaBits int
	var ecdsaCurve string
	var renewBeforeDays int
	var reissueLeadDays int
	var waitTimeout time.Duration
	var waitInterval time.Duration
	var quiet bool

	fs.StringVar(&name, "name", "", "managed certificate name")
	fs.StringVar(&commonName, "common-name", "", "certificate common name")
	fs.Var(&dnsNames, "dns-name", "additional SAN DNS name, repeatable")
	fs.StringVar(&product, "product", "RapidSSL", "regfish TLS product")
	fs.StringVar(&outputDir, "output-dir", "", "output directory for certificate material")
	fs.StringVar(&installHook, "install-hook", "", "shell command executed after successful deploy")
	fs.StringVar(&webserver, "webserver", "", "reload the given webserver after deploy: nginx, apache, or caddy")
	fs.StringVar(&webserverConfig, "webserver-config", "", "optional webserver config path used for validation/reload")
	fs.IntVar(&orgID, "org-id", 0, "optional organization id")
	fs.IntVar(&validityDays, "validity-days", defaultValidityDays, "requested certificate validity in days")
	fs.StringVar(&keyType, "key-type", config.DefaultKeyType, "private key type: rsa or ecdsa")
	fs.IntVar(&rsaBits, "rsa-bits", config.DefaultRSABits, "RSA private key size")
	fs.StringVar(&ecdsaCurve, "ecdsa-curve", config.DefaultECDSACurve, "ECDSA curve: p256, p384, or p521")
	fs.IntVar(&renewBeforeDays, "renew-before-days", config.DefaultRenewBeforeDays, "days before certificate expiry to place a renewal order")
	fs.IntVar(&reissueLeadDays, "reissue-lead-days", config.DefaultReissueLeadDays, "days before certificate expiry to trigger a reissue on long-running contracts")
	fs.DurationVar(&waitTimeout, "wait-timeout", 30*time.Minute, "maximum time to wait for issuance")
	fs.DurationVar(&waitInterval, "wait-interval", 30*time.Second, "poll interval while waiting for issuance")
	fs.BoolVar(&quiet, "quiet", false, "suppress progress output during issuance")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(commonName) == "" {
		return fmt.Errorf("--common-name is required")
	}
	if strings.TrimSpace(outputDir) == "" {
		return fmt.Errorf("--output-dir is required")
	}
	if err := config.ValidateValidityDaysAt(validityDays, time.Now().UTC()); err != nil {
		return err
	}
	if err := deploy.ValidateWebserverIntegration(deploy.WebserverIntegration{
		Kind:       webserver,
		ConfigPath: webserverConfig,
	}); err != nil {
		return err
	}
	if name == "" {
		name = strings.TrimSpace(strings.ToLower(commonName))
	}
	if existing, _ := store.FindManagedCertificate(name); existing != nil {
		return fmt.Errorf("managed certificate %q already exists", name)
	}

	client, err := a.newClient(root, store)
	if err != nil {
		return err
	}
	manager := NewManager(client, store, root.StateFile)
	if quiet {
		manager.Progress = nopProgressReporter{}
	} else {
		manager.Progress = newWriterProgressReporter(os.Stderr)
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	result, err := manager.Issue(ctx, config.ManagedCertificate{
		Name:            name,
		CommonName:      strings.TrimSpace(strings.TrimSuffix(commonName, ".")),
		DNSNames:        certcrypto.NormalizeDNSNames("", dnsNames),
		Product:         product,
		OrganizationID:  orgID,
		ValidityDays:    validityDays,
		OutputDir:       absOutputDir,
		InstallHook:     installHook,
		Webserver:       webserver,
		WebserverConfig: webserverConfig,
		KeyType:         keyType,
		RSABits:         rsaBits,
		ECDSACurve:      ecdsaCurve,
		RenewBeforeDays: renewBeforeDays,
		ReissueLeadDays: reissueLeadDays,
	}, waitTimeout, waitInterval)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s\n", result.Name, result.Message)
	fmt.Printf("certificate_id: %s\n", result.CertificateID)
	fmt.Printf("live dir: %s\n", result.LiveDir)
	return nil
}

func (a *App) runIssuePair(ctx context.Context, args []string, root rootOptions, store *config.Store) error {
	fs := flag.NewFlagSet("issue-pair", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	defaultValidityDays := config.DefaultValidityDaysAt(time.Now().UTC())

	var nameBase string
	var commonName string
	var dnsNames stringList
	var product string
	var outputDirBase string
	var installHook string
	var webserver string
	var webserverConfig string
	var orgID int
	var validityDays int
	var rsaBits int
	var ecdsaCurve string
	var renewBeforeDays int
	var reissueLeadDays int
	var waitTimeout time.Duration
	var waitInterval time.Duration
	var quiet bool

	fs.StringVar(&nameBase, "name-base", "", "managed certificate base name; certbro adds -rsa and -ecdsa")
	fs.StringVar(&commonName, "common-name", "", "certificate common name")
	fs.Var(&dnsNames, "dns-name", "additional SAN DNS name, repeatable")
	fs.StringVar(&product, "product", "RapidSSL", "regfish TLS product")
	fs.StringVar(&outputDirBase, "output-dir-base", "", "base output directory; certbro adds -rsa and -ecdsa")
	fs.StringVar(&installHook, "install-hook", "", "shell command executed after successful deploy of the ECDSA certificate")
	fs.StringVar(&webserver, "webserver", "", "reload the given webserver after both certificates are in place: nginx, apache, or caddy")
	fs.StringVar(&webserverConfig, "webserver-config", "", "optional webserver config path used for validation/reload")
	fs.IntVar(&orgID, "org-id", 0, "optional organization id")
	fs.IntVar(&validityDays, "validity-days", defaultValidityDays, "requested certificate validity in days")
	fs.IntVar(&rsaBits, "rsa-bits", config.DefaultRSABits, "RSA private key size")
	fs.StringVar(&ecdsaCurve, "ecdsa-curve", config.DefaultECDSACurve, "ECDSA curve: p256, p384, or p521")
	fs.IntVar(&renewBeforeDays, "renew-before-days", config.DefaultRenewBeforeDays, "days before certificate expiry to place a renewal order")
	fs.IntVar(&reissueLeadDays, "reissue-lead-days", config.DefaultReissueLeadDays, "days before certificate expiry to trigger a reissue on long-running contracts")
	fs.DurationVar(&waitTimeout, "wait-timeout", 30*time.Minute, "maximum time to wait for issuance")
	fs.DurationVar(&waitInterval, "wait-interval", 30*time.Second, "poll interval while waiting for issuance")
	fs.BoolVar(&quiet, "quiet", false, "suppress progress output during issuance")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(commonName) == "" {
		return fmt.Errorf("--common-name is required")
	}
	if strings.TrimSpace(outputDirBase) == "" {
		return fmt.Errorf("--output-dir-base is required")
	}
	if err := config.ValidateValidityDaysAt(validityDays, time.Now().UTC()); err != nil {
		return err
	}
	integration := deploy.WebserverIntegration{
		Kind:       webserver,
		ConfigPath: webserverConfig,
	}
	if err := deploy.ValidateWebserverIntegration(integration); err != nil {
		return err
	}

	if nameBase == "" {
		nameBase = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(commonName, ".")))
	}
	absOutputDirBase, err := filepath.Abs(outputDirBase)
	if err != nil {
		return err
	}

	rsaManaged, ecdsaManaged := buildIssuePairManagedCertificates(issuePairOptions{
		NameBase:        nameBase,
		CommonName:      strings.TrimSpace(strings.TrimSuffix(commonName, ".")),
		DNSNames:        certcrypto.NormalizeDNSNames("", dnsNames),
		Product:         product,
		OutputDirBase:   absOutputDirBase,
		OrganizationID:  orgID,
		ValidityDays:    validityDays,
		RenewBeforeDays: renewBeforeDays,
		ReissueLeadDays: reissueLeadDays,
		RSABits:         rsaBits,
		ECDSACurve:      ecdsaCurve,
		InstallHook:     installHook,
		Webserver:       integration.Kind,
		WebserverConfig: integration.ConfigPath,
	})

	for _, managed := range []config.ManagedCertificate{rsaManaged, ecdsaManaged} {
		if existing, _ := store.FindManagedCertificate(managed.Name); existing != nil {
			return fmt.Errorf("managed certificate %q already exists", managed.Name)
		}
	}

	client, err := a.newClient(root, store)
	if err != nil {
		return err
	}
	manager := NewManager(client, store, root.StateFile)
	if quiet {
		manager.Progress = nopProgressReporter{}
	} else {
		manager.Progress = newWriterProgressReporter(os.Stderr)
	}

	rsaIssue := rsaManaged
	rsaIssue.Webserver = ""
	rsaIssue.WebserverConfig = ""
	rsaIssue.InstallHook = ""
	rsaResult, err := manager.Issue(ctx, rsaIssue, waitTimeout, waitInterval)
	if err != nil {
		return err
	}

	ecdsaResult, err := manager.Issue(ctx, ecdsaManaged, waitTimeout, waitInterval)
	if err != nil {
		return err
	}

	if err := persistIssuePairSettings(store, root.StateFile, []string{rsaManaged.Name, ecdsaManaged.Name}, integration, installHook); err != nil {
		return err
	}

	fmt.Printf("%s: %s\n", rsaResult.Name, rsaResult.Message)
	fmt.Printf("  certificate_id: %s\n", rsaResult.CertificateID)
	fmt.Printf("  live dir: %s\n", rsaResult.LiveDir)
	fmt.Printf("%s: %s\n", ecdsaResult.Name, ecdsaResult.Message)
	fmt.Printf("  certificate_id: %s\n", ecdsaResult.CertificateID)
	fmt.Printf("  live dir: %s\n", ecdsaResult.LiveDir)
	return nil
}

func (a *App) runImport(ctx context.Context, args []string, root rootOptions, store *config.Store) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var certificateID string
	var name string
	var outputDir string
	var installHook string
	var webserver string
	var webserverConfig string
	var orgID int
	var keyType string
	var rsaBits int
	var ecdsaCurve string
	var renewBeforeDays int
	var reissueLeadDays int
	var privateKeyFile string
	var csrFile string

	fs.StringVar(&certificateID, "certificate-id", "", "existing TLS certificate id from the regfish API")
	fs.StringVar(&name, "name", "", "managed certificate name")
	fs.StringVar(&outputDir, "output-dir", "", "output directory for certificate material")
	fs.StringVar(&installHook, "install-hook", "", "shell command executed after successful deploy")
	fs.StringVar(&webserver, "webserver", "", "reload the given webserver after deploy: nginx, apache, or caddy")
	fs.StringVar(&webserverConfig, "webserver-config", "", "optional webserver config path used for validation/reload")
	fs.IntVar(&orgID, "org-id", 0, "optional organization id used for future new orders")
	fs.StringVar(&keyType, "key-type", config.DefaultKeyType, "private key type used for future renewals: rsa or ecdsa")
	fs.IntVar(&rsaBits, "rsa-bits", config.DefaultRSABits, "RSA private key size used for future renewals")
	fs.StringVar(&ecdsaCurve, "ecdsa-curve", config.DefaultECDSACurve, "ECDSA curve used for future renewals: p256, p384, or p521")
	fs.IntVar(&renewBeforeDays, "renew-before-days", config.DefaultRenewBeforeDays, "days before certificate expiry to place a renewal order")
	fs.IntVar(&reissueLeadDays, "reissue-lead-days", config.DefaultReissueLeadDays, "days before certificate expiry to trigger a reissue on long-running contracts")
	fs.StringVar(&privateKeyFile, "private-key-file", "", "path to the existing private key for deploying the current certificate")
	fs.StringVar(&csrFile, "csr-file", "", "optional path to the existing CSR for the current certificate")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(certificateID) == "" {
		return fmt.Errorf("--certificate-id is required")
	}
	if strings.TrimSpace(outputDir) == "" {
		return fmt.Errorf("--output-dir is required")
	}
	if err := deploy.ValidateWebserverIntegration(deploy.WebserverIntegration{
		Kind:       webserver,
		ConfigPath: webserverConfig,
	}); err != nil {
		return err
	}

	var privateKeyPEM []byte
	if strings.TrimSpace(privateKeyFile) != "" {
		raw, err := os.ReadFile(strings.TrimSpace(privateKeyFile))
		if err != nil {
			return fmt.Errorf("read private key file %s: %w", privateKeyFile, err)
		}
		privateKeyPEM = raw
	}

	var csrPEM []byte
	if strings.TrimSpace(csrFile) != "" {
		raw, err := os.ReadFile(strings.TrimSpace(csrFile))
		if err != nil {
			return fmt.Errorf("read csr file %s: %w", csrFile, err)
		}
		csrPEM = raw
	}

	client, err := a.newClient(root, store)
	if err != nil {
		return err
	}
	manager := NewManager(client, store, root.StateFile)

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	result, err := manager.Import(ctx, config.ManagedCertificate{
		Name:            name,
		OutputDir:       absOutputDir,
		InstallHook:     installHook,
		Webserver:       webserver,
		WebserverConfig: webserverConfig,
		OrganizationID:  orgID,
		KeyType:         keyType,
		RSABits:         rsaBits,
		ECDSACurve:      ecdsaCurve,
		RenewBeforeDays: renewBeforeDays,
		ReissueLeadDays: reissueLeadDays,
		CertificateID:   strings.TrimSpace(certificateID),
	}, privateKeyPEM, csrPEM)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s\n", result.Name, result.Message)
	fmt.Printf("certificate_id: %s\n", result.CertificateID)
	if result.LiveDir != "" {
		fmt.Printf("live dir: %s\n", result.LiveDir)
	}
	return nil
}

func (a *App) runRenew(ctx context.Context, args []string, root rootOptions, store *config.Store) error {
	fs := flag.NewFlagSet("renew", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var names stringList
	var force bool
	var validityDays int
	var waitTimeout time.Duration
	var waitInterval time.Duration
	var quiet bool

	fs.Var(&names, "name", "managed certificate name to renew, repeatable")
	fs.BoolVar(&force, "force", false, "force renewal even if not due")
	fs.IntVar(&validityDays, "validity-days", 0, "override requested certificate validity in days for this renewal run")
	fs.DurationVar(&waitTimeout, "wait-timeout", 30*time.Minute, "maximum time to wait for issuance")
	fs.DurationVar(&waitInterval, "wait-interval", 30*time.Second, "poll interval while waiting for issuance")
	fs.BoolVar(&quiet, "quiet", false, "suppress progress output during renewals")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if validityDays > 0 {
		if err := config.ValidateValidityDaysAt(validityDays, time.Now().UTC()); err != nil {
			return err
		}
	} else if validityDays < 0 {
		return fmt.Errorf("--validity-days must be greater than zero")
	}

	renewLock, err := lock.Acquire(renewLockPath(root))
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			fmt.Fprintln(os.Stderr, "another certbro renew process is already running; skipping")
			return nil
		}
		return err
	}
	defer renewLock.Close()

	renewStore, err := a.storeForRenew(root, store)
	if err != nil {
		return err
	}
	if len(renewStore.ManagedCertificates) == 0 {
		return fmt.Errorf("no managed certificates configured")
	}

	client, err := a.newClient(root, store)
	if err != nil {
		return err
	}
	manager := NewManager(client, renewStore, root.StateFile)
	if quiet {
		manager.Progress = nopProgressReporter{}
	} else {
		manager.Progress = newWriterProgressReporter(os.Stderr)
	}

	results, err := manager.Renew(ctx, names, force, validityDays, waitTimeout, waitInterval)
	if err != nil {
		return err
	}

	for _, result := range results {
		fmt.Printf("%s: %s", result.Name, result.Action)
		if result.Message != "" {
			fmt.Printf(" (%s)", result.Message)
		}
		fmt.Println()
		if result.Changed {
			fmt.Printf("  certificate_id: %s\n", result.CertificateID)
			fmt.Printf("  live dir: %s\n", result.LiveDir)
		}
	}
	return nil
}

func (a *App) runInstall(args []string, root rootOptions, store *config.Store) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	serviceName := "certbro"
	systemdDir := "/etc/systemd/system"
	envFile := ""
	onCalendar := "hourly"
	binaryPath := ""
	certificatesDir := root.CertificatesDir
	skipSystemctl := false

	fs.StringVar(&serviceName, "service-name", serviceName, "systemd service base name")
	fs.StringVar(&systemdDir, "systemd-dir", systemdDir, "directory for systemd unit files")
	fs.StringVar(&envFile, "env-file", envFile, "environment file path used by the systemd service")
	fs.StringVar(&onCalendar, "on-calendar", onCalendar, "systemd timer schedule")
	fs.StringVar(&binaryPath, "binary-path", binaryPath, "path to the certbro binary")
	fs.StringVar(&certificatesDir, "certificates-dir", certificatesDir, "root directory containing certbro-managed certificate directories")
	fs.BoolVar(&skipSystemctl, "skip-systemctl", false, "only write unit files, do not call systemctl")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if runtime.GOOS != "linux" {
		return fmt.Errorf("certbro install currently supports only Linux systemd")
	}
	if err := a.requireVerifiedAPIConfiguration(root, store); err != nil {
		return err
	}

	clientConfig := a.clientConfig(root, store)
	clientAPIKey := clientConfig.APIKey
	if clientAPIKey == "" {
		return fmt.Errorf("missing API key for installed service: configure one first or pass --api-key")
	}
	apiBaseURL := clientConfig.APIBaseURL
	contactEmail := clientConfig.ContactEmail
	installStateFile := root.StateFile
	if strings.TrimSpace(certificatesDir) != "" {
		defaultStateFile, err := config.DefaultPath()
		if err == nil && root.StateFile == defaultStateFile {
			installStateFile = filepath.Join(certificatesDir, "state.json")
		}
	}

	if strings.TrimSpace(binaryPath) == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		binaryPath = executable
	}
	if !filepath.IsAbs(binaryPath) {
		absBinaryPath, err := filepath.Abs(binaryPath)
		if err != nil {
			return err
		}
		binaryPath = absBinaryPath
	}

	if err := systemd.Install(systemd.Options{
		ServiceName:     serviceName,
		BinaryPath:      binaryPath,
		SystemdDir:      systemdDir,
		EnvFile:         envFile,
		OnCalendar:      onCalendar,
		StateFile:       installStateFile,
		CertificatesDir: certificatesDir,
		APIKey:          clientAPIKey,
		APIBaseURL:      apiBaseURL,
		ContactEmail:    contactEmail,
		SkipSystemctl:   skipSystemctl,
	}); err != nil {
		return err
	}

	fmt.Printf("installed systemd units for %s\n", serviceName)
	return nil
}

func (a *App) newClient(root rootOptions, store *config.Store) (*api.Client, error) {
	if err := a.requireVerifiedAPIConfiguration(root, store); err != nil {
		return nil, err
	}
	return a.newClientFromStore(root, store)
}

type clientConfig struct {
	APIKey            string
	APIBaseURL        string
	ContactEmail      string
	UserAgentInstance string
}

func (a *App) clientConfig(root rootOptions, store *config.Store) clientConfig {
	return clientConfig{
		APIKey:            firstNonEmpty(strings.TrimSpace(root.APIKey), strings.TrimSpace(os.Getenv("REGFISH_API_KEY")), strings.TrimSpace(store.APIKey)),
		APIBaseURL:        firstNonEmpty(strings.TrimSpace(root.APIBaseURL), strings.TrimSpace(os.Getenv("REGFISH_API_BASE")), strings.TrimSpace(store.APIBaseURL), api.DefaultBaseURL),
		ContactEmail:      firstNonEmpty(strings.TrimSpace(os.Getenv("CERTBRO_CONTACT_EMAIL")), strings.TrimSpace(store.ContactEmail)),
		UserAgentInstance: strings.TrimSpace(store.UserAgentInstance),
	}
}

func (a *App) newClientFromStore(root rootOptions, store *config.Store) (*api.Client, error) {
	cfg := a.clientConfig(root, store)
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("missing API key: run certbro configure --api-key to validate and store it")
	}
	userAgent := api.BuildUserAgent(api.UserAgentOptions{
		Product:      "certbro",
		Version:      a.Version,
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		ContactEmail: cfg.ContactEmail,
		Instance:     cfg.UserAgentInstance,
	})
	return api.NewClient(cfg.APIKey, cfg.APIBaseURL, userAgent)
}

func (a *App) requireVerifiedAPIConfiguration(root rootOptions, store *config.Store) error {
	cfg := a.clientConfig(root, store)
	if cfg.APIKey == "" {
		return fmt.Errorf("missing API key: run certbro configure --api-key to validate and store it")
	}
	if strings.TrimSpace(store.APIKey) == "" || store.APIKeyValidatedAt == nil {
		return fmt.Errorf("no verified API key configured: run certbro configure --api-key")
	}
	if cfg.APIKey != strings.TrimSpace(store.APIKey) {
		return fmt.Errorf("the active API key differs from the verified configured key: run certbro configure --api-key to validate and store it")
	}

	configuredBaseURL := firstNonEmpty(strings.TrimSpace(store.APIBaseURL), api.DefaultBaseURL)
	if cfg.APIBaseURL != configuredBaseURL {
		return fmt.Errorf("the active API base URL differs from the verified configured base URL: run certbro configure --api-base-url %s", cfg.APIBaseURL)
	}
	return nil
}

func (a *App) printRootUsage() {
	fmt.Fprintf(os.Stderr, "certbro %s\n\n", a.Version)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  configure   Store default API credentials and base URL")
	fmt.Fprintln(os.Stderr, "  import      Import an existing regfish TLS certificate into certbro")
	fmt.Fprintln(os.Stderr, "  install     Install a systemd service and timer for unattended renewals")
	fmt.Fprintln(os.Stderr, "  issue       Order a TLS certificate, create DCV CNAMEs, wait, and deploy")
	fmt.Fprintln(os.Stderr, "  issue-pair  Order matching RSA and ECDSA certificate variants")
	fmt.Fprintln(os.Stderr, "  renew       Reissue or re-order managed certificates when due")
	fmt.Fprintln(os.Stderr, "  update      Update stored certificate settings such as validity_days")
	fmt.Fprintln(os.Stderr, "  list        Show managed certificates from the local state file")
	fmt.Fprintln(os.Stderr, "  version     Print the certbro version")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Global flags:")
	fmt.Fprintln(os.Stderr, "  --state-file PATH")
	fmt.Fprintln(os.Stderr, "  --api-key KEY")
	fmt.Fprintln(os.Stderr, "  --api-base-url URL")
	fmt.Fprintln(os.Stderr, "  --certificates-dir PATH")
}

func (a *App) storeForRenew(root rootOptions, store *config.Store) (*config.Store, error) {
	certificatesDir := strings.TrimSpace(root.CertificatesDir)
	if certificatesDir == "" {
		return store, nil
	}

	discovered, err := config.DiscoverManagedCertificates(certificatesDir)
	if err != nil {
		return nil, err
	}

	merged := &config.Store{
		Version:           store.Version,
		APIKey:            store.APIKey,
		APIBaseURL:        store.APIBaseURL,
		ContactEmail:      store.ContactEmail,
		UserAgentInstance: store.UserAgentInstance,
	}
	seen := map[string]struct{}{}
	for _, cert := range discovered {
		key := cert.Name
		if key == "" {
			key = cert.OutputDir
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged.ManagedCertificates = append(merged.ManagedCertificates, cert)
	}
	return merged, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func renewLockPath(root rootOptions) string {
	if lockPath := strings.TrimSpace(os.Getenv("CERTBRO_RENEW_LOCK_FILE")); lockPath != "" {
		return lockPath
	}
	if certificatesDir := strings.TrimSpace(root.CertificatesDir); certificatesDir != "" {
		return filepath.Join(certificatesDir, "certbro-renew.lock")
	}
	if stateFile := strings.TrimSpace(root.StateFile); stateFile != "" {
		return filepath.Join(filepath.Dir(stateFile), "certbro-renew.lock")
	}
	return filepath.Join(os.TempDir(), "certbro-renew.lock")
}

func formatOptionalTime(ts *time.Time) string {
	if ts == nil {
		return "-"
	}
	return ts.UTC().Format(time.RFC3339)
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

type issuePairOptions struct {
	NameBase        string
	CommonName      string
	DNSNames        []string
	Product         string
	OutputDirBase   string
	OrganizationID  int
	ValidityDays    int
	RenewBeforeDays int
	ReissueLeadDays int
	RSABits         int
	ECDSACurve      string
	InstallHook     string
	Webserver       string
	WebserverConfig string
}

func buildIssuePairManagedCertificates(opts issuePairOptions) (config.ManagedCertificate, config.ManagedCertificate) {
	nameBase := strings.TrimSpace(opts.NameBase)
	outputDirBase := strings.TrimSpace(opts.OutputDirBase)

	common := config.ManagedCertificate{
		CommonName:      strings.TrimSpace(strings.TrimSuffix(opts.CommonName, ".")),
		DNSNames:        certcrypto.NormalizeDNSNames("", opts.DNSNames),
		Product:         strings.TrimSpace(opts.Product),
		OrganizationID:  opts.OrganizationID,
		ValidityDays:    opts.ValidityDays,
		InstallHook:     strings.TrimSpace(opts.InstallHook),
		Webserver:       strings.TrimSpace(opts.Webserver),
		WebserverConfig: strings.TrimSpace(opts.WebserverConfig),
		RenewBeforeDays: opts.RenewBeforeDays,
		ReissueLeadDays: opts.ReissueLeadDays,
	}

	rsaManaged := common
	rsaManaged.Name = nameBase + "-rsa"
	rsaManaged.OutputDir = outputDirBase + "-rsa"
	rsaManaged.KeyType = certcrypto.KeyTypeRSA
	rsaManaged.RSABits = opts.RSABits

	ecdsaManaged := common
	ecdsaManaged.Name = nameBase + "-ecdsa"
	ecdsaManaged.OutputDir = outputDirBase + "-ecdsa"
	ecdsaManaged.KeyType = certcrypto.KeyTypeECDSA
	ecdsaManaged.ECDSACurve = opts.ECDSACurve

	return rsaManaged, ecdsaManaged
}

func persistIssuePairSettings(store *config.Store, stateFile string, names []string, integration deploy.WebserverIntegration, installHook string) error {
	now := time.Now().UTC()
	for _, name := range names {
		managed, _ := store.FindManagedCertificate(name)
		if managed == nil {
			return fmt.Errorf("managed certificate %q not found after issue-pair", name)
		}
		managed.Webserver = strings.TrimSpace(integration.Kind)
		managed.WebserverConfig = strings.TrimSpace(integration.ConfigPath)
		managed.InstallHook = strings.TrimSpace(installHook)
		managed.UpdatedAt = now
		if err := config.SaveManagedCertificate(managed.OutputDir, *managed); err != nil {
			return err
		}
	}
	return config.Save(stateFile, store)
}
