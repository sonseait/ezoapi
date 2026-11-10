package main

import (
	"log"
	"reflect"

	"github.com/gofiber/fiber/v3"
	"github.com/sonseait/ezoapi"
)

type User struct {
	ID      uint64 `json:"id" example:"18446744073709551615"`
	Name    string `json:"name" example:"Ada"`
	Enabled bool   `json:"enabled" example:"false"`
}

func main() {
	app := fiber.New()
	api := ezoapi.New(app, ezoapi.Config{Info: ezoapi.Info{Title: "Users API", Version: "1.0.0"}})
	users := api.Group("/api/v1")
	err := users.Get("/users/:id", func(c fiber.Ctx) error {
		return c.JSON(User{ID: 18446744073709551615, Name: "Ada", Enabled: false})
	}, ezoapi.Route{
		Summary: "Get a user", Tags: []string{"Users"},
		Params: struct {
			ID uint64 `uri:"id" example:"18446744073709551615"`
		}{},
		Response:        reflect.TypeFor[User](),
		ResponseExample: User{ID: 18446744073709551615, Name: "Ada", Enabled: false},
	})
	if err != nil {
		log.Fatal(err)
	}
	app.Get("/openapi.json", api.Handler())
	log.Fatal(app.Listen(":3000"))
}
