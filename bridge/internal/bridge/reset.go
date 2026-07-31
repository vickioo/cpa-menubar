package bridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const codexResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"

func (s *Server) handleResetCredit(w http.ResponseWriter, r *http.Request) {
	var request ResetCreditRequest
	if err := decodeJSON(r, &request); err != nil || request.Confirm != "CONSUME_RESET_CREDIT" {
		writeError(w, http.StatusBadRequest, "explicit reset confirmation is required")
		return
	}

	accountID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/desktop/v1/accounts/"), "/reset-credit")
	if len(accountID) != 12 || strings.Contains(accountID, "/") {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	files, err := s.loadAuthFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read accounts")
		return
	}
	var selected *authFile
	for index := range files {
		if accountPublicID(files[index].FileName) == accountID {
			selected = &files[index]
			break
		}
	}
	if selected == nil || selected.Provider != "codex" {
		writeError(w, http.StatusNotFound, "Codex account not found")
		return
	}
	if selected.Disabled || selected.AccessToken == "" {
		writeError(w, http.StatusConflict, "account is unavailable")
		return
	}

	usage, status, err := s.fetchUsage(r.Context(), *selected)
	if err != nil || status != "ok" {
		writeError(w, http.StatusBadGateway, "unable to verify reset credits")
		return
	}
	available, applicable, err := resetCreditCounts(usage)
	if err != nil {
		writeError(w, http.StatusBadGateway, "unable to verify reset credits")
		return
	}
	if available <= 0 || applicable <= 0 {
		writeError(w, http.StatusConflict, "no applicable reset credits")
		return
	}
	if err = s.consumeCodexResetCredit(r.Context(), *selected); err != nil {
		if errors.Is(err, errResetUnauthorized) {
			writeError(w, http.StatusConflict, "account authorization must be refreshed")
			return
		}
		writeError(w, http.StatusBadGateway, "reset credit could not be consumed")
		return
	}
	writeJSON(w, http.StatusOK, ResetCreditResponse{AccountID: accountID, Status: "consumed"})
}

func resetCreditCounts(usage json.RawMessage) (int, int, error) {
	var payload struct {
		ResetCredits *struct {
			AvailableCount           int `json:"available_count"`
			ApplicableAvailableCount int `json:"applicable_available_count"`
		} `json:"rate_limit_reset_credits"`
	}
	if err := json.Unmarshal(usage, &payload); err != nil {
		return 0, 0, err
	}
	if payload.ResetCredits == nil {
		return 0, 0, errors.New("reset credit status unavailable")
	}
	return payload.ResetCredits.AvailableCount, payload.ResetCredits.ApplicableAvailableCount, nil
}

var errResetUnauthorized = errors.New("reset authorization rejected")

func (s *Server) consumeCodexResetCredit(ctx context.Context, file authFile) error {
	redeemID, err := randomUUID()
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"redeem_request_id": redeemID})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, codexResetCreditsURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+file.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal")
	if file.AccountID != "" {
		request.Header.Set("Chatgpt-Account-Id", file.AccountID)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if _, err = readLimited(response.Body, 64*1024); err != nil {
		return err
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return errResetUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("reset endpoint returned %d", response.StatusCode)
	}
	return nil
}

func randomUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(buffer)
	return hexValue[0:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32], nil
}
