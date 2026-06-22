package sip

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var digestParam = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^,\s]+))`)

func DigestAuthorization(challenge, username, password, method, uri string) (string, error) {
	params := map[string]string{}
	for _, match := range digestParam.FindAllStringSubmatch(challenge, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		params[match[1]] = value
	}
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" {
		return "", fmt.Errorf("invalid digest challenge")
	}
	ha1 := hash(username + ":" + realm + ":" + password)
	ha2 := hash(method + ":" + uri)
	qop := ""
	for _, candidate := range strings.Split(params["qop"], ",") {
		if strings.TrimSpace(candidate) == "auth" {
			qop = "auth"
			break
		}
	}
	if qop == "" {
		response := hash(ha1 + ":" + nonce + ":" + ha2)
		return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5`,
			username, realm, nonce, uri, response), nil
	}
	nc := "00000001"
	cnonceBytes := make([]byte, 8)
	_, _ = rand.Read(cnonceBytes)
	cnonce := hex.EncodeToString(cnonceBytes)
	response := hash(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5, qop=%s, nc=%s, cnonce="%s"`,
		username, realm, nonce, uri, response, qop, nc, cnonce), nil
}

func hash(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
