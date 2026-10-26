package main

// 나무증권 App Key / App Secret 보관
//
// 맥: 키체인(로그인 키체인)에만 저장해요. Go 엔진은 cgo 없이 만들기 때문에 맥에 들어 있는 /usr/bin/security 를 써요.
//   - 저장: `security -i` 에 명령을 표준 입력으로 보내요. 비밀값은 16진수(-X)로 넘겨서 명령줄(ps 로 보이는 곳)에 남지 않아요.
//   - 읽기: `security find-generic-password -w` (같은 security 프로그램이 만든 항목이라 암호 창이 뜨지 않아요)
// 시험(리눅스 · 맥 빌드 서버의 일부 시험): MFROAD_TEST_SECRETS=file 이면 Support/test-secrets.json 에 저장해요.
// 비밀값은 어떤 기록 파일에도 남기지 않아요.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const keychainService = "MFROAD"

var secretMu sync.Mutex

func testSecrets() bool { return env("TEST_SECRETS") == "file" }

func testSecretPath() string { return filepath.Join(app.support, "test-secrets.json") }

func readTestSecrets() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(testSecretPath()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func secretGet(name string) (string, error) {
	secretMu.Lock()
	defer secretMu.Unlock()
	return secretRead(name)
}

func secretRead(name string) (string, error) {
	if testSecrets() {
		return readTestSecrets()[name], nil
	}
	if runtime.GOOS != "darwin" {
		return "", errors.New("키체인은 맥에서만 쓸 수 있어요")
	}
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", keychainService, "-a", name, "-w")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 44 { // 항목 없음
			return "", nil
		}
		return "", errors.New("키체인에서 키를 읽지 못했어요")
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

func secretSet(name, value string) error {
	secretMu.Lock()
	defer secretMu.Unlock()
	if testSecrets() {
		m := readTestSecrets()
		m[name] = value
		b, _ := json.Marshal(m)
		return writeFileAtomic(testSecretPath(), b, 0o600)
	}
	if runtime.GOOS != "darwin" {
		return errors.New("키체인은 맥에서만 쓸 수 있어요")
	}
	// -U: 있으면 바꾸기 · -X: 값(16진수) · -l: 키체인 접근에 보이는 이름
	line := "add-generic-password -U -s " + keychainService + " -a " + name + " -l MFROAD-NH -X " + hex.EncodeToString([]byte(value)) + "\n"
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(line)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	cmd.Stdout = &errb
	runErr := cmd.Run()
	line = "" // 비밀값이 담긴 문자열은 더 쓰지 않아요
	// security -i 는 명령이 실패해도 끝 코드가 0일 수 있어서, 다시 읽어 확인해요
	got, err := secretRead(name)
	if runErr != nil || err != nil || got != value {
		return errors.New("키체인에 저장하지 못했어요. 맥 로그인 키체인이 잠겨 있지 않은지 확인해 주세요")
	}
	return nil
}

func secretDelete(name string) {
	secretMu.Lock()
	defer secretMu.Unlock()
	if testSecrets() {
		m := readTestSecrets()
		delete(m, name)
		b, _ := json.Marshal(m)
		writeFileAtomic(testSecretPath(), b, 0o600)
		return
	}
	if runtime.GOOS == "darwin" {
		exec.Command("/usr/bin/security", "delete-generic-password", "-s", keychainService, "-a", name).Run()
	}
}

// ---------------------------------------------------------------- 시험용 주소 바꾸기

// loadTestOverrides: 맥 빌드 서버의 시험은 앱을 `open` 으로 켜서 환경 변수를 넘길 수 없어요.
// 그래서 Support/test.json 이 있으면 그 값을 환경 변수로 써요. 주소는 이 맥 안(127.0.0.1)만 받아요.
func loadTestOverrides(support string) {
	b, err := os.ReadFile(filepath.Join(support, "test.json"))
	if err != nil {
		return
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return
	}
	for k, v := range m {
		key := "MFROAD_" + strings.ToUpper(k)
		if os.Getenv(key) != "" {
			continue
		}
		switch strings.ToUpper(k) {
		case "NH_BASE", "UPBIT_BASE", "MASTER_URL":
			if isLocalURL(v) {
				os.Setenv(key, v)
			}
		case "TEST_SECRETS":
			os.Setenv(key, v)
		}
	}
}

func isLocalURL(u string) bool {
	return strings.HasPrefix(u, "http://127.0.0.1:") || strings.HasPrefix(u, "http://localhost:")
}
