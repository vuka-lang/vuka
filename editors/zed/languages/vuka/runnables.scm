((package_clause
  (package_identifier) @run) @_
  (#set! tag vuka-package-test))

((function_declaration
  name: (_) @run @test
  (#match? @test "^Test")
  (#not-eq? @test "TestMain")) @_
  (#set! tag vuka-test))
