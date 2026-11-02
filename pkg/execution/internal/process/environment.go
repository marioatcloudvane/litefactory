package process

import "strings"

// DoctorEnvironment discards ambient provider settings, factory/forge secrets,
// proxies and user paths. A provider may explicitly append its own auth secret;
// neither the result broker nor prepared context receives that credential.
// USER and LOGNAME stay: Claude names its keychain login after the user, and
// without them it keeps a separate login under "unknown" that workflow runs
// and an owner's /login never refresh.
func DoctorEnvironment(parent []string, privateHome string) []string {
	result := []string{"HOME=" + privateHome, "TMPDIR=" + privateHome}
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if ok && (key == "PATH" || key == "LANG" || key == "LC_ALL" || key == "USER" || key == "LOGNAME") {
			result = append(result, item)
		}
	}
	return result
}
