
# Go Template LSP

Go template is incredible. But the lack of editor/IDE support is a crime. This makes the feedback loop between coding and bug detection a real challenge.

This LSP aims to tackle that issue. From now on, instantaneous diagnostics and type checking are a breeze.

You will never again need to download a dependency to replace the good and reliable standard `text/template` and `html/template` packages. Build with confidence :

- SSR web apps
- Static sites (Hugo, etc.)  
- Any project using Go templates

Below is an early version of the LSP in action (diagnostics)

![diagnostics image for Go Template](assets/examples_1.png)

## Table Of Contents

- [Features](#features)
- [Installation](#installation)
  - [Recommended](#recommended)
  - [Build From Source](#build-from-source)
- [Editor Setup](#editor-setup)
  - [Neovim](#neovim)
  - [Helix](#helix)
  - [VS Code](#vs-code)
- [Usage](#usage)
  - [Embedded Go Code](#embedded-go-code)
  - [Type Inference](#type-inference)
  - [Type Checker](#type-checker)
  - [Code Navigation](#code-navigation)
- [Roadmap](#roadmap)
- [Back Logs](#back-logs)

## Features

- Diagnostics
- Go To Definition
- Hover
- Dependency analysis of Template call

## Installation

### Recommended

```bash
go install github.com/yayolande/go-template-lsp@latest
```

### Build From Source

```bash
git clone https://github.com/yayolande/go-template-lsp
cd go-template-lsp

go get -u
go build
```

Then add the executable to your `$PATH`

## Editor Setup

### Neovim

Add the code below to your `init.lua`:

```lua
vim.api.nvim_create_autocmd("FileType", {
	pattern = "html",
	callback = function()
		vim.lsp.start({
			name = "go-template-lsp",
			cmd = { "go-template-lsp" },
			root_dir = vim.fs.dirname(vim.fs.find({ "go.mod" }, { upward = true })[1]),
		})
	end,
})
```

**NB:** For the example above, the LSP will only be launched whenever an 'html' file is opened within a GO project. To change it, look at `pattern` and `root_dir` properties

### Helix

Append to your `<config_dir>/helix/languages.toml`

```toml
[[language]]
name = "html"
# name = "gotmpl"

scope = "text.html.basic"
roots = ["go.mod"]
file-types = ["html"]
language-servers = ["go-template-lsp", "vscode-html-language-server"]

[language-server.go-template-lsp]
command = "go-template-lsp"
```

### VS Code

Coming Soon

## Usage

Go Template does not have a type system; it mainly relies on reflection and runtime check.

Because of this, the LSP has a few extra rules to enhance its type system.

> [!IMPORTANT]
> I recommend at least reading **Embedded Go Code** section and **Type Inference - Summary** to understand the most important part of the LSP

### Embedded Go Code

It is possible to embed Go code within your template file. To do so, wrap it around those special go code comment `{{/* go:code ... */}}`

```bash
{{/* go:code 
	type Input struct { // define type of the '.' variable
		name string
		age int
	}

	type Friend struct {}

	func getFriendListOf(realName string) []Friend
*/}}

{{ .name }} is {{ .age }} old, and is friend with :
{{ getFriendListOf .name }}
```

This is done to **provide type hints to functions and the `.` variable** before being able to use them.

For functions, you only need to provide their type signatures.

For the root `.` variable, you have to explicitly define the `Input` type like above (eg. `type Input struct {}`)

Note however that you do not need to provide type for the builtin functions. The LSP is aware of them.

### Type Inference

#### Summary

- **Dot Variable (.)**: Inherits types from function parameters when suitable
- **Declared Variables (:=)**: Take the type of their expression
- **Template Calls**: Disable type inference for passed variables
- **Special Cases**: Range loops have unique inference rules

<details>
  <summary><strong>Basics</strong></summary>
  <br/>

Although *go:code comment* provides type to functions, the rest of the type information is driven by type inference. Type inference rules are as follow :

- **Type inference on `.` variable**: When using a `.` variable as part of an argument of a function, if the said variable doesn't have a prior type, then the variable inherit the type of the parameter
- **Type inference on variable declaration (:=)**: A newly declared variable take the type of its expression
- **Type inference is always disabled whenever template call are involved `{{ template "name" $var }}`**

```bash
{{/* go:code 
	type Company struct {
		ID int
		Name string
		EmployeeNames []string
	}

	func getCompanyDetails(int) Company
	func hasPaidTaxes(string) bool
	func taxMath(float64) float64
*/}}

{{ $my_company := getCompanyDetails .WorkPlaceId  }}

{{ .name }} work at {{ $my_company.Name }} and 
ought {{ if hasPaidTaxes .name }} nothing {{ else }} {{ taxMath .salary }} {{ end }}

best friend is {{ .Friends.Best_Friend }}
```

For this example, `.` variable doesn't have a type since it was not defined in *go:code comment block*. Therefore, the LSP will infer as much as possible its type

Below is the evaluated type of all used variables

```go
var $my_company Company
var . Input

type Company struct {
  ID int
  Name string
  EmployeeNames []string
}

type Input struct {
  WorkPlaceId int
  name string
  salary float64
  Fiends []struct {
    Best_Friend any
  }
}
```

You have to be careful about one thing though, `$` variable type can never be inferred

```bash
{{/* go:code 
	func countCharacter(string) int
*/}}

{{ countCharacter $ }} // cannot infer type of '$', it will keep its default 'any' type
```

In this example above the type is `var . any`. The type checker will complains since the function `countCharacter(string) int` expected a `string` as argument but got `any`

However the LSP is smart enough to make a late type resolution. Thus if a variable type is unknown, the LSP will wait until the end of the scope before evaluating its type

```bash
{{/* go:code func countCharacter(string) int */}}

{{ countCharacter $ }} // no error here since the type is inferred below
{{ countCharacter . }} // type inferred here
```

Note that in this context `$` is an "alias" for the `.` variable

</details>

<details>
  <summary><strong>With Block</strong></summary>
  <br/>

The `{{ with $var }}` command take a variable and make it the new context (`.` variable) within its scope. The same inference rule apply for that `.` variable

```bash
{{/* go:code 
	func greeting(name string) string
	func integerToString(int) string
*/}}

{{ $info := .unknown }}
{{ .alt }}

{{ with $info }} // although '$info' variable type cannot be inferred

	{{ greeting .Name }} // now '.' refer to '$info', so inference work
	{{ integerToString .Vistor_Count }}

  {{ greeting $info.Surname }} // error, '$info' type cannot be inferred (only '.' variable is inferred)
{{ end }}
```

So the final type of the `$info` and the root `.` variable is as follow

```go
var $info Info
var . Input

type Info struct {
  Name string
  Vistor_Count int
}

type Input struct {
  unknown Info
  alt any
}
```

This pattern is especially useful to give complex type to `$` variables

</details>

<details>
  <summary><strong>Range Block</strong></summary>
  <br/>

While inferring a variable type within a loop (`{{ range ... }}`), the default behavior is to infer to either a slice or a map

```bash
{{- /* go:code func escapeHTML(string) string */ -}}

{{ range .tags }}
	{{ escapeHTML . }} // infer the element type of the iterable '.tags'
{{ end }}
```

Although at first `.tags` is of type `any`, since it is within `{{ range ...}}` it become an iterable. The LSP assume this iterable to be a slice (`[]any`) since no key is explicitly provided (more on this later).

Furthermore, the elements of `.tags` have been inferred as `string` with the help of the function `escapeHTML(string) string`. Combining those 2 inferences, we get the type below

```go
var . Input

type Input struct {
  tags []string
}
```

Things can become even more complex as below

```bash
{{- /* go:code 
	func capitalize(string) string
	func toUpper(string) string
*/ -}}

{{ range $key, $val := .dictionary }}
	{{ capitalize . }} : // '$val' inferred as 'string'. Note that '.' is an alias for '$val' in range loop
	{{ toUpper $key }}   // '$key' is exceptionally inferred as 'string' as well
{{ end }}
```

`$val` is inferred as `string` because within the `{{ range ...}}` scope, `.` variable is an alias for `$val`.

However something strange happened to `$key`. We have stated earlier that type inference only happen for `.` variable, but not for `$` variables of any kind.

Well that is true, however this is the only exception to this rule.

Therefore, if a `key` variable is explicitly provided while declaring (**:=**) a variable, although the variable is a `$` variable, it behave internally as a `.` variable. Thus making inference rule apply to it.

Finally, the type of `.dictionary` variable will be an iterable, specifically a *map* because this time around a key as been provided

```go
var $key string
var $val string
var . Input

type Input struct {
  dictionary map[string]string
}
```

You might be surprised to hear that indirectly, inference rule apply on `$` within the `{{ range }}` expression. Take a look at the example below

```bash
{{/* go:code 
	type Student struct {
		Name string
		Age int
		Grade float32
		Major string
	}

	func isInTopTen (int) bool
	func displayStudentInfo(Student) string
*/}}

{{ $students := .students_sorted_by_grade }}

{{ range $rank, $student := $students }}
	{{ if isInTopTen $rank }}
		{{ displayStudentInfo . }}
	{{ end }}
{{ end }}
```

The variable `$rank` get its type from `isInTopTen(int) bool`. Similarly, `$student` derives its type from `displayStudentInfo(Student) string`. 

Now what about `$students` ? This time around it is not a `.` variable so no inference, right ?

Well kinda ! Remember the `{{ with $var }}` block ? The `{{ with }}` block transformed `$var` to a `.` variable and inference was applied to it. The same goes for `{{ range }}` block. Since `$rank` and `.` have inference enabled by default within the `{{ range }}` block, then the LSP can infer the type of `$students` since it knows the key and element type

Here are the type of all variables

```go
var $rank int
var $student Student

var $studens []Student
var . Input

type Input struct { students_sorted_by_grade []Student }
```

Note however that `$students` type was inferred because of the **declaration operator :=** as well. If it was the **assignment operator =** as below, the same outcome will not stand 

```bash
{{/* go:code
	func inter(int) int
	func stringer(string) string
*/}}

{{ $key := .key }}
{{ $val := .val }}

{{ range $key, $val = .students }} // .students type is not inferred, instead is evaluated againts an anonymous iterable of key '$key' and element type '$val'
	{{ inter $key }} // however inference rule still apply 
	{{ stringer . }} // same here
{{ end }}
```

The final variable types is as follow

```go
var $key int
var $val string
var . Input

type Input struct {
  students any
}
```

Therefore, for a matter of simplicity **refrain from using `assignment operator =`**  when declaring key and value within loop (`{{ range $key, $val = expression }}`).

**Use the declaration operator `:=` as much as possible**

One last thing. The iterable inferred by the LSP follow those rules:

- Infer a **slice** if the *key type* is either `int` or `any`
- Otherwise infer a **map**

</details>

### Type Checker

Most of the time, the LSP performs a strict type check. However, a more permissive check is done while dealing with template call `{{ template "templateName" $var }}`. In this mode, the type checker enforces that all fields of `"templateName"` are also present within `$var` type. In other word, it tests whether `"templateName" type` is a subset of `$var type`; a compatibility check of sort.

```bash
{{/* go:code
	type Input struct { Name any }
*/}}

{{ .Name }}

{{ template "country" . }}   // error, field type mismatch between 'var .Name any' and '.Name string'

{{ template "continent" . }} // OK

{{ template "planet" . }}    // error, field '.Age' is missing


{{ define "country" }}
	{{/* go:code type Input struct {Name string; Age int} */}}
{{ end }}

{{ define "continent" }}
	{{ .Name }}
{{ end }}

{{ define "planet" }}
	{{ .Name }}
	{{ .Age }}
{{ end }}
```

### Code Navigation

So far, **Hover** and **Go To Definition** are available. They work on functions, methods, template call, and variables (inferred or not)

```bash
{{- /* go:code type Input struct { Name string; Age int } */ -}}

// You can interact with "content" and "." below
{{ template "content" . }}

// even with this
{{ define "content" }}
	Name = {{ .Name }}
{{ end }}
```

Others are coming soon enough

## Roadmap

- [x] Diagnostics
- [x] Hover
- [x] Go To Definition
- [ ] Type System
- [ ] Better Editor Support (VS Code, Nvim distribution, Vim)
- [ ] Auto-Completion
- [ ] Semantic Highlighting
- [ ] Code Formatter
- [ ] Better ergonomics for navigation
- [ ] Integration with Go Code

## Back Logs

- [ ] Alter parse tree and tokens so that partial tree is returned when error occurs, rather than returning nil (this will help returning better diagnostics, especially for group node)
- [ ] Fix bug for which diagnostics are not displaying for 'define' group node sometimes
- [ ] Builtin functions types are not implemented yet, and make the program crash
- [ ] Extend supported files to '.tmpl', '.tpl', '.gohtml' (while reading from disk & LSP)
- [ ] Go To Implementation, Declaration, Type
- [ ] Special command for faster navigation & symbol information
- [ ] Documentation on how to use LSP features
- [ ] Demo video or GIF

