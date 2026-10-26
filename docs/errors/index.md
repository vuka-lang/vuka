# Errors

Every diagnostic the vuka toolchain reports has a stable code: the
compiler, `vuka vet`, the editor and the libraries' checks at boot. `E` is
an error and `W` a warning; the first two digits name the area. Each page
says what is wrong, why, and shows the fix. `vuka explain E0201` prints the
same page in the terminal.

## The compiler

| Code | |
|---|---|
| [E0001](./E0001) | Internal compiler error |
| [E0002](./E0002) | Type error |
| [E0003](./E0003) | Syntax error |
| [E0004](./E0004) | The Vuka runtime is too old |
| [E0005](./E0005) | A dependency doesn't compile |
| [E0006](./E0006) | A .templ file doesn't compile |

## Syntax, JSX and the language

| Code | |
|---|---|
| [E0101](./E0101) | JSX syntax error |
| [E0102](./E0102) | JSX needs a target |
| [E0103](./E0103) | Two JSX targets |
| [E0104](./E0104) | The JSX target can't be used |
| [E0105](./E0105) | Can't lower a JSX element |
| [E0106](./E0106) | A component's attributes don't fit |
| [E0107](./E0107) | An event handler doesn't fit |
| [E0108](./E0108) | No such ref or attribute value |
| [E0109](./E0109) | A field reference names no field |
| [E0110](./E0110) | Misplaced ? |
| [E0111](./E0111) | Malformed match |
| [E0112](./E0112) | A pattern doesn't fit its subject |
| [E0113](./E0113) | match isn't exhaustive |
| [E0114](./E0114) | Unreachable case |
| [E0115](./E0115) | Malformed enum |
| [E0116](./E0116) | Malformed static |
| [E0117](./E0117) | Can't infer a static's type |
| [E0118](./E0118) | Static initialization cycle |
| [E0119](./E0119) | A static or field can't be used this way |
| [E0120](./E0120) | Conflicting overloads |
| [E0121](./E0121) | Ambiguous or unresolvable overload |
| [E0122](./E0122) | Can't infer a type argument |
| [E0124](./E0124) | Code that can't run in the browser |
| [E0125](./E0125) | ui.State used with the wrong type |
| [E0126](./E0126) | Bad vuka.File |
| [E0127](./E0127) | ui.HTML isn't given one value per label |
| [E0128](./E0128) | A value doesn't fit its hole |
| [E0129](./E0129) | Attributes and a hand-written schema method |
| [E0130](./E0130) | A hook attribute that is no hook |
| [E0131](./E0131) | A function an around decorator can't wrap |
| [E0132](./E0132) | A value isn't written as its label |
| [E0141](./E0141) | A template has two scripts or two scoped stylesheets |
| [E0142](./E0142) | A template file has no package clause, or another package's |
| [E0143](./E0143) | An unknown element or template |
| [E0144](./E0144) | A tag leaves out a parameter |
| [E0146](./E0146) | A template file's Go declares no struct |
| [E0147](./E0147) | A template file declares more than one struct |
| [E0148](./E0148) | A template file has Go and no markup |
| [E0149](./E0149) | A state written where nothing sends it to the page |
| [E0150](./E0150) | A write the browser can't run |
| [E0151](./E0151) | Markup outside a .vuka.html file |
| [E0152](./E0152) | A template file declares no struct above its markup |
| [E0153](./E0153) | A second template in a template file |
| [E0154](./E0154) | An island page that breaks its rules |
| [E0155](./E0155) | Tracked state used in a .go file |
| [E0156](./E0156) | A Go script with no end tag |
| [E0157](./E0157) | A Go script that is not the file's first element |
| [E0158](./E0158) | A tag leaves out a required field |
| [E0159](./E0159) | A model or for attribute names a field the form lacks |
| [W0101](./W0101) | A bare name is a field and a name of the package |
| [W0102](./W0102) | A live list's items have no key |
| [W0103](./W0103) | A stateful component in a loop has no key |
| [W0105](./W0105) | A hand-written use of uigen |
| [W0106](./W0106) | A hand-written ui.HTML or ui.IslandPage |
| [W0107](./W0107) | A .vuka.html file in the Go-header form |

## Decorators and attributes

| Code | |
|---|---|
| [E0201](./E0201) | Malformed attribute |
| [E0202](./E0202) | Attribute in the wrong place |
| [E0203](./E0203) | An attribute must start its own line |
| [E0204](./E0204) | Attribute without a declaration |
| [E0205](./E0205) | Bad parameter attribute |
| [E0206](./E0206) | Bad field attribute |
| [E0207](./E0207) | Attribute on the wrong kind of declaration |
| [E0208](./E0208) | Malformed decorator declaration |
| [E0209](./E0209) | A decorator composes itself |
| [E0210](./E0210) | A typed decorator in a composition |
| [E0211](./E0211) | Type decorator on a generic type |
| [E0212](./E0212) | Decorated init |
| [E0213](./E0213) | A declarer can't take this declaration |
| [E0214](./E0214) | Bad @export |

## Islands and live pages

| Code | |
|---|---|
| [E0301](./E0301) | Malformed island import |
| [E0302](./E0302) | Island imported twice |
| [E0303](./E0303) | Can't read an island component |
| [E0304](./E0304) | Can't read an island's props |
| [E0305](./E0305) | The JSX target can't import islands |
| [E0306](./E0306) | Malformed <ui.Island> |
| [E0307](./E0307) | Not an island component |
| [E0308](./E0308) | Bad hydration directive |
| [E0309](./E0309) | An island's attributes don't fit |
| [E0310](./E0310) | Island component not found |
| [E0311](./E0311) | Island component is ambiguous |
| [E0312](./E0312) | Two islands share a name |
| [E0320](./E0320) | Bad @ui.Island arguments |
| [E0321](./E0321) | Bad ui.Layout |
| [E0322](./E0322) | Bad bind: tag |
| [E0323](./E0323) | Bad defer: tag |
| [E0324](./E0324) | A deferred field has no loader |
| [E0326](./E0326) | Two handles share a name |
| [E0328](./E0328) | Bad validate: tag |
| [E0329](./E0329) | Bad ui.Live embedding |
| [E0330](./E0330) | An island page under @web.Live isn't live |
| [E0331](./E0331) | A page backed by a component isn't a GET |
| [E0332](./E0332) | @web.Get on a type without @ui.Island |
| [E0334](./E0334) | Two Update methods take URL parameters |
| [E0335](./E0335) | A LiveSchema field that isn't the page's |
| [E0336](./E0336) | A live page hook that doesn't fit |
| [E0337](./E0337) | A hook found by its name |
| [E0338](./E0338) | A hook declared by the constructor |
| [E0340](./E0340) | A path parameter the page doesn't have |
| [W0301](./W0301) | A live page routed as a stateless one |
| [W0302](./W0302) | A live page configured by struct tags |
| [W0306](./W0306) | A hand-written LiveSchema |
| [W0307](./W0307) | Two components keeping one URL parameter |
| [W0308](./W0308) | A script names a ref, state or event its shape lacks |
| [W0309](./W0309) | A form method names no field of the form |
| [W0310](./W0310) | A form field has a placeholder and no label |
| [W0311](./W0311) | A form embeds a database model |

## Routes and the web app

| Code | |
|---|---|
| [E0401](./E0401) | A route is declared twice |
| [E0402](./E0402) | A frontend serves a route a handler serves |
| [E0403](./E0403) | A handler isn't a function |
| [E0404](./E0404) | A route's attribute needs what the app doesn't have |
| [E0405](./E0405) | A handler parameter can't be bound |
| [E0406](./E0406) | A handler returns what the app can't write |
| [E0407](./E0407) | No provider for a dependency |
| [E0408](./E0408) | A type is provided twice |
| [E0409](./E0409) | A dependency cycle |
| [E0410](./E0410) | A constructor has the wrong shape |
| [E0411](./E0411) | A cron is wrong |
| [E0412](./E0412) | A cron is declared twice |
| [E0413](./E0413) | Bad TrustedProxies entry |
| [E0414](./E0414) | Implementation[T] finds no one type |
| [E0415](./E0415) | An environment variable is missing or invalid |
| [E0416](./E0416) | A .env file doesn't parse |
| [E0417](./E0417) | A view doesn't fit its template |
| [E0418](./E0418) | A service or controller that isn't a struct |
| [E0419](./E0419) | A dependency field the URL binds too |
| [E0420](./E0420) | A removed way to declare a dependency |
| [E0421](./E0421) | Two context processors give one variable |
| [E0422](./E0422) | A variable not every main gives |
| [E0423](./E0423) | A context processor variable read without a context |
| [E0424](./E0424) | An undefined name near a context processor variable |
| [E0425](./E0425) | A list entry that is no context processor |
| [E0426](./E0426) | Personal values sent to the browser |
| [E0427](./E0427) | A web.Var read the app doesn't give |
| [E0428](./E0428) | Context processors that need each other |
| [W0401](./W0401) | A name shadowing a context processor variable |
| [W0402](./W0402) | A direct read where a processor variable would do |
| [W0403](./W0403) | A control hidden by a permission its event doesn't check |

## Data and the ORM

| Code | |
|---|---|
| [E0501](./E0501) | A Schema field isn't the model's |
| [E0502](./E0502) | A nil Schema attribute |
| [E0503](./E0503) | A Schema field behind a pointer |

## Frontend and toolchain

| Code | |
|---|---|
| [E0601](./E0601) | The frontend isn't built |
| [E0602](./E0602) | Node.js or the package manager isn't installed |
| [E0603](./E0603) | A frontend dependency is missing |
| [E0604](./E0604) | An asset declaration can't be read |
| [E0605](./E0605) | An asset file isn't there |
| [E0606](./E0606) | Two pages share an asset scope |
| [E0607](./E0607) | vite.config doesn't use the vuka plugin |
| [E0608](./E0608) | TypeScript 7 can't build Vue islands |
| [E0609](./E0609) | The islands have no frontend build to go into |
| [E0610](./E0610) | npm is older than 11 |

## Folder routes

| Code | |
|---|---|
| [E0701](./E0701) | Two pages on one path |
| [E0702](./E0702) | Two routes match the same paths |
| [E0703](./E0703) | Something is below a catch-all folder |
| [E0704](./E0704) | A bracketed name names no path parameter |
| [E0705](./E0705) | A path parameter's type can't be in a URL |
| [E0706](./E0706) | A name vuka declares for a page is taken |
| [E0707](./E0707) | A folder route and a hand-written route share a path |
| [E0708](./E0708) | A folder's layout or Guard is malformed |
| [E0709](./E0709) | A method handler doesn't fit |
| [E0710](./E0710) | A folder's Middleware is malformed |
| [E0711](./E0711) | Nothing binds a path parameter |
| [E0712](./E0712) | The fields binding a path parameter disagree on its type |
| [E0713](./E0713) | A parameter is both a file and a folder |
| [E0714](./E0714) | A Params struct makes no parameter folder |
| [E0715](./E0715) | A Layout type wraps no folder |
| [E0716](./E0716) | A Go file the folder's package can't hold |
| [E0717](./E0717) | Two Go files declare one method func |
| [E0718](./E0718) | A file is in the way of pages/route.go |
| [E0719](./E0719) | A page's struct is declared nowhere |
| [E0720](./E0720) | A page declared outside pages/ |
| [E0721](./E0721) | A page whose markup is written in Go |
| [E0722](./E0722) | Two routes on one URL through a route group |
| [E0723](./E0723) | A name in parentheses that names no route group |
| [E0724](./E0724) | The home page outside the root layouts |
| [W0701](./W0701) | No main package serves the folder routes |
| [W0702](./W0702) | A hand-written folder-route option |
| [W0703](./W0703) | A layout reading the request |