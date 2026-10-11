package web

import (
	"net/http"
	"time"
)

// The 两步验证 card on the settings page. Every response re-renders the whole
// settings page and the forms select #settings out of it, so the card always
// shows the state the service is actually in.

func (s *Server) postTOTPBegin(w http.ResponseWriter, r *http.Request) {
	account, err := s.svc.AdminUsername()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, _, err := s.svc.BeginTOTP(account); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderSettingsWith(w, r, "", nil)
}

func (s *Server) postTOTPEnable(w http.ResponseWriter, r *http.Request) {
	codes, err := s.svc.EnableTOTP(r.FormValue("code"), time.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderSettingsWith(w, r, "两步验证已开启", map[string]any{"RecoveryCodes": codes})
}

func (s *Server) postTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DisableTOTP(r.FormValue("code"), time.Now()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderSettingsWith(w, r, "两步验证已关闭", nil)
}

func (s *Server) postTOTPRecovery(w http.ResponseWriter, r *http.Request) {
	codes, err := s.svc.RegenerateRecoveryCodes(r.FormValue("code"), time.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderSettingsWith(w, r, "已生成新的恢复码，旧的全部作废", map[string]any{"RecoveryCodes": codes})
}

// totpSettings is the card's part of the settings page data.
func (s *Server) totpSettings(data map[string]any) error {
	status, err := s.svc.TOTPStatus()
	if err != nil {
		return err
	}
	data["TOTP"] = status
	if status.Enabled {
		return nil
	}
	account, err := s.svc.AdminUsername()
	if err != nil {
		return err
	}
	secret, uri, err := s.svc.PendingTOTP(account)
	if err != nil || secret == "" {
		return err
	}
	qr, err := qrDataURI(uri)
	if err != nil {
		return err
	}
	data["TOTPSecret"] = secret
	data["TOTPQR"] = qr
	return nil
}
