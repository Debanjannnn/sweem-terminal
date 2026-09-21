// Command sync-policy reconciles the live Privy policy against the spec built
// from the executor's market allowlist. It is a separate binary because it is
// the only place in services/wallet allowed to read PRIVY_APP_SECRET: the API
// process refuses to boot if it can see one.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	walletpolicy "github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

const privyAPI = "https://api.privy.io"

// the kuru router proxy, mirroring executor/src/config.rs
const defaultRouter = "0x7EFbE105Ca7415dE98F96622173458ac1c054630"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sync-policy:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		marketsPath = flag.String("markets", "executor/kuru-markets.json", "path to the executor market allowlist")
		envPath     = flag.String("env", ".env", "dotenv file to read credentials from if they are not already in the environment")
		name        = flag.String("name", "sweem terminal executor allowlist", "policy name")
		router      = flag.String("router", "", "kuru router address; defaults to KURU_ROUTER or the committed proxy")
		policyID    = flag.String("policy-id", "", "existing policy to reconcile; defaults to PRIVY_POLICY_ID")
		out         = flag.String("out", "", "optional file to write the resulting policy id to")
		apply       = flag.Bool("apply", false, "write the spec to Privy; without it the tool only reports drift")
	)
	flag.Parse()

	config.LoadDotEnv(*envPath)

	markets, err := walletpolicy.LoadMarkets(*marketsPath)
	if err != nil {
		return err
	}
	if *router == "" {
		*router = config.GetEnv("KURU_ROUTER", defaultRouter)
	}
	if *policyID == "" {
		*policyID = config.GetEnv("PRIVY_POLICY_ID", "")
	}
	ownerID := ""
	if *apply && *policyID == "" {
		if ownerID = config.GetEnv("PRIVY_KEY_QUORUM_ID", ""); ownerID == "" {
			return errors.New("PRIVY_KEY_QUORUM_ID is not set, and a new policy needs an owner")
		}
	}

	spec, err := Build(*name, ownerID, *router, markets)
	if err != nil {
		return err
	}
	doc, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(doc))

	if *policyID == "" && !*apply {
		fmt.Fprintln(os.Stderr, "\nno policy id: nothing to reconcile against. Re-run with -apply to create this policy.")
		return nil
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	if *policyID == "" {
		id, err := c.createPolicy(spec)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\ncreated policy %s\n", id)
		return writeID(*out, id)
	}

	live, err := c.getPolicy(*policyID)
	if err != nil {
		return err
	}
	drift := Diff(live, spec)
	if len(drift) == 0 {
		fmt.Fprintf(os.Stderr, "\npolicy %s matches the spec\n", *policyID)
		return writeID(*out, *policyID)
	}
	fmt.Fprintf(os.Stderr, "\npolicy %s has drifted from the spec:\n", *policyID)
	for _, d := range drift {
		fmt.Fprintln(os.Stderr, "  -", d)
	}
	if !*apply {
		return fmt.Errorf("%d difference(s) between the live policy and the spec; re-run with -apply to write the spec", len(drift))
	}
	if err := c.updatePolicy(*policyID, spec); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "updated policy %s\n", *policyID)
	return writeID(*out, *policyID)
}

func writeID(path, id string) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(id+"\n"), 0o644)
}

type client struct {
	appID  string
	secret string
	key    *ecdsa.PrivateKey
	http   *http.Client
}

func newClient() (*client, error) {
	appID, secret := config.GetEnv("PRIVY_APP_ID", ""), config.GetEnv("PRIVY_APP_SECRET", "")
	if appID == "" || secret == "" {
		return nil, errors.New("PRIVY_APP_ID and PRIVY_APP_SECRET must both be set to talk to Privy")
	}
	key, err := parseAuthKey(config.GetEnv("PRIVY_AUTHORIZATION_PRIVATE_KEY", ""))
	if err != nil {
		return nil, err
	}
	return &client{appID: appID, secret: secret, key: key, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func parseAuthKey(raw string) (*ecdsa.PrivateKey, error) {
	b64 := strings.TrimPrefix(strings.TrimSpace(raw), "wallet-auth:")
	if b64 == "" {
		return nil, errors.New("PRIVY_AUTHORIZATION_PRIVATE_KEY is not set, so no request could be authorized")
	}
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("PRIVY_AUTHORIZATION_PRIVATE_KEY is not valid base64")
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("PRIVY_AUTHORIZATION_PRIVATE_KEY is not a PKCS#8 key")
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("PRIVY_AUTHORIZATION_PRIVATE_KEY is not a P-256 key")
	}
	return ec, nil
}

func (c *client) createPolicy(p Policy) (string, error) {
	var got Policy
	if err := c.do(http.MethodPost, privyAPI+"/v1/policies", p, &got); err != nil {
		return "", err
	}
	if got.ID == "" {
		return "", errors.New("Privy created the policy but returned no id")
	}
	return got.ID, nil
}

func (c *client) updatePolicy(id string, p Policy) error {
	body := struct {
		Name  string `json:"name"`
		Rules []Rule `json:"rules"`
	}{p.Name, p.Rules}
	return c.do(http.MethodPatch, privyAPI+"/v1/policies/"+id, body, nil)
}

func (c *client) getPolicy(id string) (Policy, error) {
	var got Policy
	err := c.do(http.MethodGet, privyAPI+"/v1/policies/"+id, nil, &got)
	return got, err
}

func (c *client) do(method, url string, body, into any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = b
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.appID, c.secret)
	req.Header.Set("privy-app-id", c.appID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	sig, err := c.sign(method, url, payload)
	if err != nil {
		return err
	}
	req.Header.Set("privy-authorization-signature", sig)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s: %s", method, url, resp.Status, strings.TrimSpace(string(raw)))
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(raw, into)
}

func (c *client) sign(method, url string, body []byte) (string, error) {
	payload, err := canonical(signedPayload(c.appID, method, url, body))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	der, err := ecdsa.SignASN1(rand.Reader, c.key, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// signedPayload is what Privy hashes. An empty object body canonicalizes to
// "", not {}: sign the wrong one and every GET is rejected.
func signedPayload(appID, method, url string, body []byte) map[string]any {
	var b any = ""
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" && trimmed != "{}" {
		b = json.RawMessage(body)
	}
	return map[string]any{
		"version": 1,
		"method":  method,
		"url":     strings.TrimRight(url, "/"),
		"body":    b,
		"headers": map[string]any{"privy-app-id": appID},
	}
}

// canonical is RFC-8785 JCS enough for this payload: encoding/json sorts
// object keys, and HTML escaping is off so no character is rewritten.
func canonical(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
