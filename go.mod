module github.com/wau/wau-go-sdk

go 1.23

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/wau/circuit v0.6.0
)

replace github.com/wau/circuit => ../wau-circuit
