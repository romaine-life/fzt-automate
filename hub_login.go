package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// With Hub Login Security on (romaine-life/lights LIG-3), the hub's admin pages redirect
// to /login until a form sign-in sets its HUBSESSION cookie. home-sync (at-home-tools)
// writes the login from OpenBao (secret/home/hubitat) to ~\.hubitat\login.json.
var hubLoginPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hubitat", "login.json")
}

// hubLoginError is the hub's login missing or refused, as opposed to the hub not answering.
type hubLoginError struct{ msg string }

func (e hubLoginError) Error() string { return e.msg }

// newHubClient keeps the hub's session cookie and stops at a redirect to its sign-in page,
// so the caller can sign in and retry instead of reading the login form as a reply.
func newHubClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL.Path == "/login" {
			return http.ErrUseLastResponse
		}
		return nil
	}}
}

// hubLoginRedirect is the hub sending a request to its sign-in page.
func hubLoginRedirect(resp *http.Response) bool {
	if resp.StatusCode/100 != 3 {
		return false
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	return err == nil && loc.Path == "/login"
}

func hubSignIn(ctx context.Context, client *http.Client) error {
	file := hubLoginPath()
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return hubLoginError{fmt.Sprintf("the hub asks for its login and %s doesn't exist; home-sync writes it from OpenBao", file)}
	}
	if err != nil {
		return err
	}
	var login struct{ Username, Password string }
	if err := json.Unmarshal(b, &login); err != nil || login.Username == "" || login.Password == "" {
		return hubLoginError{fmt.Sprintf("%s holds no hub username and password", file)}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{"username": {login.Username}, "password": {login.Password}, "submit": {"Login"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubitatBase+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// The reply's own redirect says whether it worked, so it isn't followed.
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 3 || hubLoginRedirect(resp) {
		return hubLoginError{fmt.Sprintf("the hub refused the login in %s (%s)", file, resp.Status)}
	}
	return nil
}
