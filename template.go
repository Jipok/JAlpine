package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/validator"
)

type JTemplate struct {
	compiled string              // Fully "compiled" template after recursive processing of include directives and other actions
	version  string              // Last modification time among all used files
	deps     map[string]struct{} // All files that participated in forming the result

	mainFile      string
	libsMap       map[string]string
	lastCheck     time.Time
	checkInterval time.Duration
}

//go:embed helpers.js
var helperJS string

// NewJTemplate creates a new JTemplate by loading and compiling a template from a file
// mainFile - path to the main template file
func NewJTemplate(mainFile string, libsMap map[string]string) (*JTemplate, error) {
	t := JTemplate{
		checkInterval: 2 * time.Second,
		mainFile:      mainFile,
		libsMap:       libsMap,
		deps:          make(map[string]struct{}),
	}

	err := t.Update()
	t.updateVersion()
	return &t, err
}

// Recompile template
func (t *JTemplate) Update() error {
	// Avoid checking the file system on every call
	if time.Since(t.lastCheck) < t.checkInterval {
		return nil
	}
	t.lastCheck = time.Now()

	// Returns true if version was updated
	if !t.updateVersion() {
		return nil
	}

	t.deps = make(map[string]struct{})
	content, err := t.loadTemplate(t.mainFile)
	if err != nil {
		log.Printf("Error during update template `%s`: %s", t.mainFile, err)
		return err
	}
	content = injectExternalLibs(content, t.libsMap)
	t.compiled = content
	return nil
}

// Version = latest modification time among the main file and any includes
func (t *JTemplate) updateVersion() bool {
	var lastModTime time.Time
	for filepath := range t.deps {
		info, _ := os.Stat(filepath)
		modTime := info.ModTime()
		if modTime.After(lastModTime) {
			lastModTime = modTime
		}
	}

	newVersion := lastModTime.Format(time.DateTime)
	needUpdate := t.version != newVersion
	t.version = newVersion
	return needUpdate
}

// loadTemplate loads a file by filePath, adds sourceURL to <script> blocks
// and processes include directives (<% ... %>) recursively
func (t *JTemplate) loadTemplate(filePath string) (string, error) {
	t.deps[filePath] = struct{}{}

	bytesContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	content := string(bytesContent)
	// Process include directives recursively
	processed, err := t.processIncludes(content, filepath.Dir(filePath))
	if err != nil {
		return "", err
	}
	// Process <script x-data="..."> tags transformation
	processed = processXDataScripts(processed)
	// Insert sourceURL comments in <script> blocks using the file's base name
	processed = addSourceURL(processed, filepath.Base(filePath))
	return processed, nil
}

// processIncludes finds all occurrences of <% include %> in the content data and replaces
// them with the content of the corresponding files (recursively). If no extension is specified in the directive,
// it's added as ".html". The insertion is wrapped with special comments.
func (t *JTemplate) processIncludes(content string, currentDir string) (string, error) {
	re := regexp.MustCompile(`<%\s*(.*?)\s*%>`)
	matches := re.FindAllStringSubmatchIndex(content, -1)
	if matches == nil {
		return content, nil
	}

	var builder bytes.Buffer
	prevEnd := 0

	// Iterate over all `include`
	for _, match := range matches {
		start := match[0]
		end := match[1]
		fileName := content[match[2]:match[3]]
		if filepath.Ext(fileName) == "" {
			fileName += ".html"
		}
		// Write before `include`
		builder.WriteString(content[prevEnd:start])

		includePath := filepath.Join(currentDir, fileName)
		includedContent, err := t.loadTemplate(includePath)
		if err != nil {
			return "", fmt.Errorf("error including %s: %v", fileName, err)
		}
		// Wrap included with comment
		wrapped := fmt.Sprintf("\n<!-- BEGIN %s -->\n%s\n<!-- END %s -->", fileName, includedContent, fileName)
		builder.WriteString(wrapped)

		prevEnd = end
	}
	// Write after `include`
	builder.WriteString(content[prevEnd:])
	return builder.String(), nil
}

// addSourceURL finds all <script> tags in content and inserts a sourceURL comment with the file name
func addSourceURL(content, filename string) string {
	const scriptTag = "<script>"
	result := content
	startIdx := 0
	for {
		idx := strings.Index(result[startIdx:], scriptTag)
		if idx == -1 {
			break
		}
		idx += startIdx
		// Helper for in browser debugger
		urlComment := fmt.Sprintf("\n//# sourceURL=%s\n", filename)
		// Insert after <script>.
		result = result[:idx+len(scriptTag)] + urlComment + result[idx+len(scriptTag):]
		startIdx = idx + len(scriptTag) + len(urlComment)
	}
	return result
}

// processXDataScripts finds <script> tags with the x-data attribute and replaces them with
// a tag without x-data, inside which is a wrapper for Alpine.data
// This function transforms:
//
//	<script x-data="componentName"> ({ script content }) </script>
//
// into:
//
//	<script>
//	  document.addEventListener('alpine:init', () => {
//	      Alpine.data('componentName', () => {
//	          ...script content...
//	      });
//	  });
//	</script>
func processXDataScripts(content string) string {
	// (?s) enables the dot to match newlines.
	re := regexp.MustCompile(`(?s)<script([^>]*)x-data="([^"]+)"([^>]*)>(.*?)</script>`)
	// Replace all occurrences with the desired format.
	replacement := `<script> document.addEventListener('alpine:init', () => { Alpine.data('$2', () => 
$4 ) });
</script>`
	return re.ReplaceAllString(content, replacement)
}

// Execute runs the template, integrating component data and js helpers.
// The method looks for the closing </body> tag and inserts integration code before it.
func (t *JTemplate) Execute(w io.Writer, data map[string]interface{}) error {
	t.Update()

	// Split data by components.
	componentData := make(map[string]map[string]interface{})
	componentData["main"] = make(map[string]interface{})
	for k, v := range data {
		if strings.Contains(k, "::") {
			parts := strings.SplitN(k, "::", 2)
			comp, key := parts[0], parts[1]
			if _, exists := componentData[comp]; !exists {
				componentData[comp] = make(map[string]interface{})
			}
			componentData[comp][key] = v
		} else {
			componentData["main"][k] = v
		}
	}

	componentData["main"]["currentVersion"] = t.version
	componentData["main"]["availVersion"] = t.version

	compDataJSON, err := json.Marshal(componentData)
	if err != nil {
		return err
	}

	// Form an integration block with data and js helpers
	integrationScript := fmt.Sprintf(`
<script>
	//# sourceURL=helpers.js
	// Set component data for Alpine
	window._componentData = %s;
%s
</script>
</body>`, compDataJSON, helperJS)

	// Insert the integration script before the closing </body> tag.
	// TODO can be optimized and instead of replace just write the first and second parts
	var output string
	if strings.Contains(t.compiled, "</body>") {
		output = strings.Replace(t.compiled, "</body>", integrationScript, 1)
	} else {
		output = t.compiled + integrationScript
	}

	_, err = w.Write([]byte(output))
	return err
}

///////////////////////////////////////////////////////////////////////////////

// For useful autocomplete EnsureStaticLibs in IDE
type EnsureLibsEntry struct {
	Name    string
	BaseURL string
}

var (
	AlpineJS = EnsureLibsEntry{
		Name:    "alpinejs",
		BaseURL: "https://unpkg.com/alpinejs",
	}

	AlpinePersist = EnsureLibsEntry{
		Name:    "alpinejs-persist",
		BaseURL: "https://unpkg.com/@alpinejs/persist",
	}

	AlpineCollapse = EnsureLibsEntry{
		Name:    "alpinejs-collapse",
		BaseURL: "https://unpkg.com/@alpinejs/collapse",
	}

	AlpineFocus = EnsureLibsEntry{
		Name:    "alpinejs-focus",
		BaseURL: "https://unpkg.com/@alpinejs/focus",
	}

	AlpineAnchor = EnsureLibsEntry{
		Name:    "alpinejs-anchor",
		BaseURL: "https://unpkg.com/@alpinejs/anchor",
	}

	AlpineSort = EnsureLibsEntry{
		Name:    "alpinejs-sort",
		BaseURL: "https://unpkg.com/@alpinejs/sort",
	}

	AlpineAutoAnimate = EnsureLibsEntry{
		Name:    "alpinejs-autoanimate",
		BaseURL: "https://cdn.jsdelivr.net/npm/@marcreichel/alpine-auto-animate@latest/dist/alpine-auto-animate.min.js",
	}

	TailwindCSS = EnsureLibsEntry{
		Name:    "tailwindcss",
		BaseURL: "https://unpkg.com/@tailwindcss/browser@4",
	}
)

// EnsureStaticLibs checks for the presence of each required file in the static folder by pattern,
// where the file name contains a version (for example, "alpinejs@*.min.js"). If the file is not found,
// a request is made to unpkg to determine the current version and download the necessary file.
//
// The function returns a map where the key is the library identifier (for example, "alpinejs"),
// and the value is the local file name (with version number).
func EnsureStaticLibs(staticDir string, plugins ...EnsureLibsEntry) (map[string]string, error) {
	err := os.MkdirAll(staticDir, os.ModePerm)
	if err != nil {
		return nil, err
	}

	// Если плагины не указаны, используем только Alpine.js
	if len(plugins) == 0 {
		plugins = []EnsureLibsEntry{AlpineJS}
	}

	libsMap := make(map[string]string)
	for _, plugin := range plugins {
		pattern := filepath.Join(staticDir, plugin.Name+"@*.js")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}

		if len(matches) > 0 {
			// File exists — use its base name.
			baseName := filepath.Base(matches[0])
			libsMap[plugin.Name] = baseName
			continue
		}

		// File not found — determine version via unpkg.
		// Use HEAD request with redirection disabled.
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Head(plugin.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed HEAD for %s: %v", plugin.BaseURL, err)
		}

		location := resp.Header.Get("Location")
		if location == "" {
			log.Printf("Can't determine version. No redirect location for %s", plugin.BaseURL)
			location = "@latest"
		}

		// Use the last index of the '@' character to extract the version
		idx := strings.LastIndex(location, "@")
		if idx == -1 || idx == len(location)-1 {
			return nil, fmt.Errorf("unexpected redirect format for %s: %s", plugin.BaseURL, location)
		}
		versionPart := location[idx+1:]

		// If "/" is present, keep only the part before "/"
		if slashIdx := strings.Index(versionPart, "/"); slashIdx != -1 {
			versionPart = versionPart[:slashIdx]
		}

		// Form a local file name including the version, for example "alpinejs@3.14.8.min.js"
		localFileName := fmt.Sprintf("%s@%s%s", plugin.Name, versionPart, ".js")
		localPath := filepath.Join(staticDir, localFileName)

		fmt.Printf("Downloading %s @ %s...\n", plugin.Name, versionPart)
		if err := downloadFile(plugin.BaseURL, localPath); err != nil {
			return nil, fmt.Errorf("failed to download %s: %v", plugin.Name, err)
		}
		libsMap[plugin.Name] = localFileName
	}
	return libsMap, nil
}

// injectExternalLibs inserts references to external libraries (Tailwind CSS, AlpineJS, AlpineJS Persist)
// into the provided HTML. It sorts the libraries so that the ones with the longest names appear first,
// and for JavaScript libraries (except for "tailwindcss") it adds the "defer" attribute.
func injectExternalLibs(html string, libsMap map[string]string) string {
	var tags []string

	// Create a slice of keys (library names)
	keys := make([]string, 0, len(libsMap))
	for k := range libsMap {
		keys = append(keys, k)
	}

	// Sort keys by descending order of length; if equal, sort alphabetically
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})

	// Iterate over the sorted keys and create corresponding tags
	for _, name := range keys {
		filename := libsMap[name]
		ext := strings.ToLower(filepath.Ext(filename))

		switch ext {
		case ".css":
			// For CSS files, add a link tag
			tags = append(tags, fmt.Sprintf(`<link rel="stylesheet" href="/static/%s">`, filename))
		case ".js":
			// For JS files, add the "defer" attribute if the library is not "tailwindcss"
			deferAttr := ""
			if strings.ToLower(name) != "tailwindcss" {
				deferAttr = " defer"
			}
			tags = append(tags, fmt.Sprintf(`<script src="/static/%s"%s></script>`, filename, deferAttr))
		}
	}

	// Join all tags with newline separator
	injection := strings.Join(tags, "\n")

	// Insert the injection before </head> if present, otherwise before </body>, else append at the end
	if strings.Contains(html, "</head>") {
		return strings.Replace(html, "</head>", injection+"\n</head>", 1)
	}
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", injection+"\n</body>", 1)
	}
	return html + injection
}

// downloadFile downloads the content from the specified URL and saves it to dest.
func downloadFile(url, dest string) error {
	// Ensure the destination directory exists.
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	// Perform HTTP GET request.
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: %s (status: %s)", url, resp.Status)
	}
	// Create destination file.
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

///////////////////////////////////////////////////////////////////////////////

var validate = validator.New()

func (t *JTemplate) Error(w http.ResponseWriter, errMsg string) {
	t.Update()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"main::error":        errMsg,
		"main::availVersion": t.version,
	})
}

func (t *JTemplate) JSON(w http.ResponseWriter, data map[string]interface{}) error {
	t.Update()
	w.Header().Set("Content-Type", "application/json")
	data["main::availVersion"] = t.version
	return json.NewEncoder(w).Encode(data)
}

// decodeAndValidate decodes the JSON body into an instance of T and validates it using go-playground/validator.
// Returns a pointer to T and false if an error occurred.
func DecodeAndValidate[T any](t *JTemplate, w http.ResponseWriter, r *http.Request) (*T, bool) {
	var data T
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		t.Error(w, "Invalid request "+err.Error())
		return nil, false
	}
	if err := validate.Struct(data); err != nil {
		t.Error(w, err.Error())
		return nil, false
	}
	return &data, true
}
