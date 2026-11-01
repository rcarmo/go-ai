package durable

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestMainSearchIndependentFieldScores(t *testing.T) {
	idf := math.Log(1 + 0.5/1.5)
	for _, tc := range []struct {
		name, query, id, text, ns string
		want                      float64
	}{
		{"name only", "cat", "mcp__cat", "", "", 4 * idf},
		{"namespace only", "cat", "x", "", "cat", 0.5 * idf},
		{"namespace repeated is set", "cat", "x", "", "cat cat cat", 0.5 * idf},
		{"text only", "cat", "x", "cat cat", "", idf * 2 * 2.2 / (2 + 1.2)},
		{"uncovered IDF contributes bonus", "cat missing", "mcp__cat", "", "", 4*idf + math.Log(4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rankDocuments(tc.query, []SearchDocument{{tc.id, tc.text}}, []string{tc.ns}, 1)
			if len(got) != 1 || math.IsNaN(got[0].Score) || math.IsInf(got[0].Score, 0) || math.Abs(got[0].Score-tc.want) > 1e-14 {
				t.Fatalf("score=%v want %.17g", got, tc.want)
			}
		})
	}
	for _, tc := range []struct{ input, want string }{{"\ufeffThread-Read\ufeff", "thread_read"}, {"\u0085Thread_Read\u0085", "_thread_read_"}, {"😀", "__"}} {
		if got := searchNormalizeName(tc.input); got != tc.want {
			t.Errorf("normalize %q=%q want %q", tc.input, got, tc.want)
		}
	}
}

// Reference: 81f3cd41 packages/durable/test/search.test.ts. Unicode dictionary
// segmentation is tracked separately; these fixtures exercise ASCII contracts.
func TestMainSearchASCIITokens(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{"listIssues for the GitHub_repo", []string{"list", "issue", "git", "hub", "repo"}},
		{"searches queries HTTPServer", []string{"search", "query", "http", "server"}},
		{"repo.issues/search:queries", []string{"repo", "issue", "search", "query"}},
		{"can do not", []string{"can", "do", "not"}},
		{" \t，。!? 😀 ", []string{}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := searchTokens(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokens=%v want %v", got, tc.want)
			}
		})
	}
}

func TestMainSearchFieldRanking(t *testing.T) {
	type doc struct{ name, text, namespace string }
	base := []doc{{"mcp__github__list_issues", "List issues in a repository. state open or closed", ""}, {"mcp__github__create_pull_request", "Open a pull request.", ""}, {"mcp__linear__search_issues", "Search Linear issues by text.", ""}, {"mcp__docs__search", "Search the documentation.", ""}}
	named := []doc{{"mcp__app__queue_read", "Read a queued message of this thread.", ""}, {"mcp__dev-app__thread_read", "Read durable state and a paginated timeline from any thread. The default view returns user messages, assistant messages and plans. Continue with afterPosition until hasMore is false.", ""}}
	ns := "mcp__peekaboo Peekaboo controls macOS apps by app name."
	peek := []doc{{"mcp__peekaboo__see", "Capture the app window of the peekaboo app and describe the app.", ns}, {"mcp__peekaboo__menu", "Click a menu item of an app.", ns}, {"mcp__peekaboo__app", "Launch, quit, relaunch, focus, hide, or switch applications. Targets an application by name, bundle identifier, or process ID, and can wait until it is ready.", ns}, {"mcp__app__queue_read", "Read a queued message of this thread.", ""}, {"mcp__dev-app__thread_read", "Read durable state and a paginated timeline from any thread. The default view returns user messages, assistant messages and plans.", ""}}
	coverage := []doc{{"mcp__peekaboo__app", "Launch, quit, focus, or switch applications by name or bundle identifier.", ""}, {"mcp__peekaboo__see", "Capture the app window and describe the UI elements of the app.", ""}, {"mcp__peekaboo__window", "Move, resize, or close a window of an app.", ""}, {"mcp__peekaboo__image", "Take a screenshot of the screen, an app, or a window.", ""}}
	cases := []struct {
		name, query string
		docs        []doc
		limit       int
		want        []string
	}{
		{"relevance", "issue", base, 8, []string{"mcp__linear__search_issues", "mcp__github__list_issues"}},
		{"plural", "pull requests", base, 1, []string{"mcp__github__create_pull_request"}},
		{"limit", "search", base, 1, []string{"mcp__docs__search"}},
		{"text", "closed", base, 8, []string{"mcp__github__list_issues"}},
		{"unknown", "kubernetes", base, 8, []string{}}, {"stopword", "the", base, 8, []string{}},
		{"namespace", "kubernetes", []doc{{"mcp__x__run", "Run it.", "mcp__x Kubernetes cluster tools"}}, 8, []string{"mcp__x__run"}},
		{"partial name", "read", named, 1, []string{"mcp__app__queue_read"}},
		{"name weight", "deploy", []doc{{"mcp__ci__status", "Deploy status: last deploy, deploy log, deploy errors.", ""}, {"mcp__ci__deploy_release", "Start a release of the current branch to an environment, run the migrations, switch traffic over, and wait until the new version is live and healthy.", ""}}, 1, []string{"mcp__ci__deploy_release"}},
		{"local bonus", "peekaboo app", peek, 1, []string{"mcp__peekaboo__app"}},
		{"local bonus extra terms", "open the peekaboo app tool", peek, 1, []string{"mcp__peekaboo__app"}},
		{"local suffix bonus", "read thread_read state", peek, 1, []string{"mcp__dev-app__thread_read"}},
		{"text without whole name", "describe window", peek, 1, []string{"mcp__peekaboo__see"}},
		{"coverage before score", "screenshot window", coverage, 1, []string{"mcp__peekaboo__image"}},
		{"coverage before bonus", "describe app", coverage, 1, []string{"mcp__peekaboo__see"}},
		{"exact before coverage", "window", coverage, 1, []string{"mcp__peekaboo__window"}},
	}
	for _, query := range []string{"thread_read", "dev_app__thread_read", "mcp__dev-app__thread_read", "Thread_Read"} {
		cases = append(cases, struct {
			name, query string
			docs        []doc
			limit       int
			want        []string
		}{"exact " + query, query, named, 1, []string{"mcp__dev-app__thread_read"}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := []CodemodeToolDescription{}
			namespaces := []CodemodeNamespace{}
			plain := []SearchDocument{}
			hasNamespace := false
			for _, d := range tc.docs {
				tools = append(tools, CodemodeToolDescription{Name: d.name, Description: d.text, InputSchema: JSON{}})
				plain = append(plain, SearchDocument{d.name, d.text})
				if d.namespace != "" {
					hasNamespace = true
					name, description, _ := strings.Cut(d.namespace, " ")
					namespaces = append(namespaces, CodemodeNamespace{Name: name, Description: description, Prefix: d.name})
				}
			}
			assert := func(label string, ranked []RankedDocument) {
				t.Helper()
				got := []string{}
				for _, r := range ranked {
					got = append(got, r.ID)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s names=%v want %v", label, got, tc.want)
				}
			}
			assert("codemode", SearchCodemodeTools(tc.query, tools, namespaces, "", tc.limit))
			if !hasNamespace {
				assert("public legacy layout", RankDocuments(tc.query, plain, tc.limit))
			}
		})
	}
}
