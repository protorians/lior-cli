package audit

import "testing"

// The JSX heuristic must tell a real element from a type argument: every
// module service writes `this.get<ContactVm>(…)`, and a rule that flags those
// makes the audit unusable (it failed 13/14 first-party modules).
func TestJSXTagREIgnoresTypeArguments(t *testing.T) {
	notJSX := []string{
		"return await this.get<FetchResponseInterface<ContactVm>>('/crm/contacts', options);",
		"const items: Array<string> = [];",
		"this.post<FetchResponseInterface<unknown>>('/x', {body});",
		"const map = new Map<string, number>();",
		"function f<T extends Foo>(x: T) { return x; }",
		"type Pair<A, B> = {a: A; b: B};",
	}
	for _, source := range notJSX {
		if jsxTagRE.MatchString(source) {
			t.Errorf("faux positif JSX sur %q", source)
		}
	}
}

func TestJSXTagREDetectsElements(t *testing.T) {
	isJSX := []string{
		"// @jsx react\nconst el = <div>hello</div>;",
		"return <ModuleView/>;",
		"return (\n  <StockView/>\n);",
		"return <DataGrid rows={rows}/>;",
		"const el = <div className=\"grid\">x</div>;",
		"return <><Row a={1}/><Row a={2}/></>;",
		"return rows.map((r) => <Row key={r.id} value={r}/>);",
	}
	for _, source := range isJSX {
		if !jsxTagRE.MatchString(source) {
			t.Errorf("JSX non détecté dans %q", source)
		}
	}
}
