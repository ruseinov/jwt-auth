package jwt

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// return is (authTokenString, refreshTokenString, err)
func (a *Auth) extractTokenStringsFromReq(r *http.Request) (string, string, *jwtError) {
	// read cookies
	if a.options.BearerTokens {
		// tokens are not in cookies
		if r.Header.Get("Content-Type") == "application/json" {
			// tokens are in the body
			content, err := ioutil.ReadAll(r.Body)
			if err != nil {
				a.myLog("Err decoding bearer tokens json \n" + err.Error())
				// a.errorHandler.ServeHTTP(w, r)
				return "", "", newJwtError(errors.New("Internal Server Error"), 500)
			}
			// write back to the body so it can be used elsewhere
			r.Body = ioutil.NopCloser(bytes.NewReader(content))

			var bearerTokens bearerTokensStruct
			err = json.Unmarshal(content, &bearerTokens)
			if err != nil {
				a.myLog("Err decoding bearer tokens json \n" + err.Error())
				// a.errorHandler.ServeHTTP(w, r)
				return "", "", newJwtError(errors.New("Internal Server Error"), 500)
			}

			return bearerTokens.Auth_Token, bearerTokens.Refresh_Token, nil
		} else {
			// tokens are form encoded
			// Note: we don't check for errors here, because we will check if the token is valid, later
			r.ParseForm()
			return strings.Join(r.Form["Auth_Token"], ""), strings.Join(r.Form["Refresh_Token"], ""), nil
		}
	} else {
		AuthCookie, authErr := r.Cookie("AuthToken")
		if authErr == http.ErrNoCookie {
			a.myLog("Unauthorized attempt! No auth cookie")
			return "", "", newJwtError(errors.New("No auth cookie"), 401)
		} else if authErr != nil {
			// a.myLog(authErr)
			return "", "", newJwtError(errors.New("Internal Server Error"), 500)
		}

		RefreshCookie, refreshErr := r.Cookie("RefreshToken")
		if refreshErr == http.ErrNoCookie {
			a.myLog("Unauthorized attempt! No refresh cookie")
			return "", "", newJwtError(errors.New("No refresh cookie"), 401)
		} else if refreshErr != nil {
			a.myLog(refreshErr)
			return "", "", newJwtError(errors.New("Internal Server Error"), 500)
		}

		return AuthCookie.Value, RefreshCookie.Value, nil
	}
}

func extractCsrfStringFromReq(r *http.Request) (string, *jwtError) {
	csrfString := r.FormValue("X-CSRF-Token")

	if csrfString != "" {
		return csrfString, nil
	}

	csrfString = r.Header.Get("X-CSRF-Token")
	if csrfString != "" {
		return csrfString, nil
	}

	auth := r.Header.Get("Authorization")
	csrfString = strings.Replace(auth, "Basic", "", 1)
	csrfString = strings.Replace(csrfString, " ", "", -1)
	if csrfString == "" {
		return csrfString, newJwtError(errors.New("No CSRF string"), 401)
	} else {
		return csrfString, nil
	}
}

func (a *Auth) setCredentialsOnResponseWriter(w http.ResponseWriter, c *credentials) *jwtError {
	authTokenString, err := c.AuthToken.Token.SignedString(a.signKey)
	if err != nil {
		return newJwtError(err, 500)
	}
	refreshTokenString, err := c.RefreshToken.Token.SignedString(a.signKey)
	if err != nil {
		return newJwtError(err, 500)
	}

	if a.options.BearerTokens {
		// tokens are not in cookies
		setHeader(w, "Auth_Token", authTokenString)
		setHeader(w, "Refresh_Token", refreshTokenString)
	} else {
		// tokens are in cookies
		// note: don't use an "Expires" in auth cookies bc browsers won't send expired cookies?
		authCookie := http.Cookie{
			Name:  "AuthToken",
			Value: authTokenString,
			// Expires:  time.Now().Add(a.options.AuthTokenValidTime),
			HttpOnly: true,
			Secure:   !a.options.IsDevEnv,
		}
		http.SetCookie(w, &authCookie)

		refreshCookie := http.Cookie{
			Name:     "RefreshToken",
			Value:    refreshTokenString,
			Expires:  time.Now().Add(a.options.RefreshTokenValidTime),
			HttpOnly: true,
			Secure:   !a.options.IsDevEnv,
		}
		http.SetCookie(w, &refreshCookie)
	}

	authTokenClaims, ok := c.AuthToken.Token.Claims.(*ClaimsType)
	if !ok {
		a.myLog("Cannot read auth token claims")
		return newJwtError(errors.New("Cannot read token claims"), 500)
	}
	refreshTokenClaims, ok := c.RefreshToken.Token.Claims.(*ClaimsType)
	if !ok {
		a.myLog("Cannot read refresh token claims")
		return newJwtError(errors.New("Cannot read token claims"), 500)
	}

	w.Header().Set("X-CSRF-Token", c.CsrfString)
	// note @adam-hanna: this may not be correct when using a sep auth server?
	//    							 bc it checks the request?
	w.Header().Set("Auth-Expiry", strconv.FormatInt(authTokenClaims.StandardClaims.ExpiresAt, 10))
	w.Header().Set("Refresh-Expiry", strconv.FormatInt(refreshTokenClaims.StandardClaims.ExpiresAt, 10))

	return nil
}

func (a *Auth) buildCredentialsFromRequest(r *http.Request, c *credentials) *jwtError {
	authTokenString, refreshTokenString, err := a.extractTokenStringsFromReq(r)
	if err != nil {
		return newJwtError(err, 500)
	}

	csrfString, err := extractCsrfStringFromReq(r)
	if err != nil {
		return newJwtError(err, 500)
	}

	err = a.buildCredentialsFromStrings(csrfString, authTokenString, refreshTokenString, c)
	if err != nil {
		return newJwtError(err, 500)
	}

	return nil
}

func (a *Auth) myLog(stoofs interface{}) {
	if a.options.Debug {
		log.Println(stoofs)
	}
}

func setHeader(w http.ResponseWriter, header string, value string) {
	w.Header().Set(header, value)
}