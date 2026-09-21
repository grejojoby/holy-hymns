package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5"
)

const appleManualRevocationURL = "https://support.apple.com/en-us/102571"

func (s *Service) appleSigningKey() (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(s.cfg.ApplePrivateKey, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("Apple signing key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("Apple signing key must be PKCS8")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("Apple signing key must use P-256")
	}
	return key, nil
}
func (s *Service) appleConfigured() bool {
	if len(s.cfg.AppleClientIDs) == 0 || s.cfg.AppleTeamID == "" || s.cfg.AppleKeyID == "" {
		return false
	}
	if _, err := s.appleSigningKey(); err != nil {
		return false
	}
	_, err := s.mailCipher()
	return err == nil
}
func (s *Service) appleClientSecret(clientID string) (string, error) {
	if !s.appleConfigured() || !slices.Contains(s.cfg.AppleClientIDs, clientID) {
		return "", errors.New("Apple server authorization is not configured")
	}
	key, err := s.appleSigningKey()
	if err != nil {
		return "", err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", s.cfg.AppleKeyID))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"iss": s.cfg.AppleTeamID, "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "aud": "https://appleid.apple.com", "sub": clientID})
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}
func (s *Service) appleRequest(ctx context.Context, endpoint string, values url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	response, err := s.appleClient.Do(req)
	if err != nil {
		return nil, errors.New("Apple could not be reached")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(body) > 64*1024 {
		return nil, errors.New("invalid Apple response")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Apple request failed with status %d", response.StatusCode)
	}
	return body, nil
}
func (s *Service) exchangeApple(ctx context.Context, in socialInput, expected socialClaims) (string, error) {
	secret, err := s.appleClientSecret(expected.Audience)
	if err != nil {
		return "", err
	}
	body, err := s.appleRequest(ctx, s.appleTokenURL, url.Values{"client_id": {expected.Audience}, "client_secret": {secret}, "code": {in.AuthorizationCode}, "grant_type": {"authorization_code"}})
	if err != nil {
		return "", err
	}
	var tokens struct {
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err = json.Unmarshal(body, &tokens); err != nil || tokens.RefreshToken == "" || tokens.IDToken == "" || len(tokens.RefreshToken) > 8192 {
		return "", errors.New("Apple did not supply refresh credentials")
	}
	verified, err := s.socialVerifier(ctx, "apple", tokens.IDToken, in.Nonce)
	if err != nil || verified.Subject != expected.Subject || verified.Audience != expected.Audience {
		return "", errors.New("Apple code identity does not match the verified sign-in")
	}
	return tokens.RefreshToken, nil
}
func (s *Service) encryptAppleGrant(userID, clientID, refresh string) ([]byte, error) {
	aead, err := s.mailCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, []byte(refresh), []byte("apple-refresh:"+userID+":"+clientID)), nil
}
func (s *Service) decryptAppleGrant(userID, clientID string, encrypted []byte) (string, error) {
	aead, err := s.mailCipher()
	if err != nil {
		return "", err
	}
	if len(encrypted) < aead.NonceSize() {
		return "", errors.New("Apple grant is missing")
	}
	plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte("apple-refresh:"+userID+":"+clientID))
	if err != nil {
		return "", errors.New("Apple grant cannot be decrypted")
	}
	return string(plain), nil
}
func (s *Service) storeAppleGrant(ctx context.Context, tx pgx.Tx, userID string, in socialInput, claims socialClaims) error {
	refresh, err := s.exchangeApple(ctx, in, claims)
	if err != nil {
		return err
	}
	encrypted, err := s.encryptAppleGrant(userID, claims.Audience, refresh)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE provider_identities SET encrypted_refresh_token=$2,token_client_id=$3 WHERE user_id=$1 AND provider='apple'`, userID, encrypted, claims.Audience)
	return err
}

// Missing historical grants must not prevent deletion. Apple explicitly requires
// erasure with manual revocation guidance in that case (TN3194). For a stored grant,
// temporary service failures retain the local account so the user can retry.
func (s *Service) revokeAppleGrant(ctx context.Context, tx pgx.Tx, userID string) (manual bool, err error) {
	var encrypted []byte
	var clientID *string
	err = tx.QueryRow(ctx, `SELECT encrypted_refresh_token,token_client_id FROM provider_identities WHERE user_id=$1 AND provider='apple' FOR UPDATE`, userID).Scan(&encrypted, &clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(encrypted) == 0 || clientID == nil {
		return true, nil
	}
	refresh, err := s.decryptAppleGrant(userID, *clientID, encrypted)
	if err != nil {
		return false, err
	}
	secret, err := s.appleClientSecret(*clientID)
	if err != nil {
		return false, err
	}
	_, err = s.appleRequest(ctx, s.appleRevokeURL, url.Values{"client_id": {*clientID}, "client_secret": {secret}, "token": {refresh}, "token_type_hint": {"refresh_token"}})
	return false, err
}
