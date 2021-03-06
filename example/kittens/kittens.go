package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/tilteng/go-api-framework/api_framework"
	"github.com/tilteng/go-app-context/app_context"
	"github.com/tilteng/go-errors/errors"
	"github.com/tilteng/go-request-tracing/request_tracing"
)

// Our controller! It embeds a Controller.
type KittensController struct {
	*api_framework.Controller
}

// Track our created kittens in memory for this example
var kittens = map[string]*Kitten{}

// ErrorClasses
var ErrInvalidKittenID = errors.NewErrorClass(
	"ErrInvalidKittenID",
	"ERR_ID_INVALID_KITTEN_ID",
	400,
	"Invalid kitten id specified",
)
var ErrKittenNotFound = errors.NewErrorClass(
	"ErrKittenNotFound",
	"ERR_ID_KITTEN_NOT_FOUND",
	404,
	"No kitten found with that id",
)

// Used for deserializing POST data. jsonapi spec says you should use
// { "data": { "attributes": { ... } } }
type createKittenBody struct {
	Data kittenData `json:"data"`
}
type kittenData struct {
	Kitten Kitten `json:"attributes"`
}

// Used for API responses
type Kitten struct {
	Id    *api_framework.UUID `json:"id,omitempty"`
	Name  string              `json:"name"`
	Color string              `json:"color,omitempty"`
}

func (self *KittensController) AddKitten(ctx context.Context) {
	rctx := self.RequestContext(ctx)

	body_obj := &createKittenBody{}
	// ReadBody() is a method on the Controller struct. It handles
	// deserializing the body into whatever object you pass. If you're
	// using the json schema middleware, the body has already been validated
	// against the schema by this point.
	self.ReadBody(ctx, &body_obj)

	kitten := &body_obj.Data.Kitten
	kitten.Id = self.GenUUID()
	if kitten.Id == nil {
		panic("uuid generation failed")
	}
	kittens[kitten.Id.String()] = kitten

	rctx.LogInfof("Created kitten with ID %s", kitten.Id)

	// WriteResponse() is a method on the Controller struct. It handles
	// serializing your data according to Accept: header and returing the
	// response. POST routes automatically send back a 201 status code.
	// See GET example below to see how you can return a differnt code.
	self.WriteResponse(ctx, body_obj)
}

func (self *KittensController) GetKitten(ctx context.Context) {
	// RequestContext() handles taking a generic context and turning it
	// into the framework's RequestContext struct. This struct is used
	// to grab route vars, etc.
	rctx := self.RequestContext(ctx)

	// The route was defined as /kittens/{id}. This is how you pull the
	// ID. I'm considering making a convenience method on Controller that
	// combines the above call with this call.
	id, _ := rctx.RouteVar("id")

	uuid := self.UUIDFromString(id)
	if uuid == nil {
		self.WriteResponse(
			rctx,
			ErrInvalidKittenID.New(rctx, "kitten id should be a uuid4 string"),
		)
		return
	}
	kitten, ok := kittens[uuid.String()]
	if !ok {
		self.WriteResponse(
			rctx,
			ErrKittenNotFound.New(
				rctx,
				"kitten id '"+uuid.String()+"' does not exist",
			),
		)
		return
	}
	self.WriteResponse(rctx, &createKittenBody{
		Data: kittenData{
			Kitten: *kitten,
		},
	})
}

func registerKittens(c *api_framework.Controller) (err error) {
	kittens := &KittensController{c}
	c.POST("/kittens", kittens.AddKitten,
		// Optional arguments. If you're using the json schema middleware,
		// it will look for a *JSONSchemaOpts struct with Name set to
		// something other than "". `Name` should be the name of a file
		// under the JSONSchemaFilePath (see controller_opts) excluding its
		// .json suffix. When this route is called, the body of data will
		// be validated against the schema found in the json file.
		c.JSONSchemaOpts("create-kitten"),
	)
	// For more efficient routing, you can create a sub-path
	kittens_router := c.SubRouterForPath("/kittens")
	// ...and then define routes on that subpath.. This is actually
	// "/kittens/{id}"
	kittens_router.GET("/{id}", kittens.GetKitten)
	return
}

func main() {
	errors.SetNewErrorHandler(errors.NewErrorHandlerFn(
		api_framework.NewErrorHandler,
	))

	// AppContext is global application state
	app_context, err := app_context.NewAppContext("kittens")
	if err != nil {
		log.Fatal(err)
	}

	port := app_context.ServicePort()
	if port == 0 {
		port = 31337
	}

	json_file_path := app_context.JSONSchemaFilePath()
	if len(json_file_path) == 0 {
		json_file_path = "./schemas"
	}

	ext_base_url := app_context.BaseExternalURL()
	if len(ext_base_url) == 0 {
		ext_base_url = fmt.Sprintf("http://localhost:%d", port)
	}

	// Use AppContext's logger to set up a logger that logs request
	// IDs. Then update AppContext's logger.
	req_trace_manager := request_tracing.NewRequestTraceManager()
	req_trace_manager.SetBaseLogger(app_context.Logger().BaseLogger())
	logger := req_trace_manager.Logger()
	app_context.SetLogger(logger)

	controller_opts := api_framework.NewControllerOpts(app_context)
	// BaseAPIURL is used to specify the real externally reachable URL. This
	// is used for returning paths to json schemas via the Link: header
	controller_opts.BaseAPIURL = ext_base_url
	// Directory containing json schema files to load. Must end in .json
	controller_opts.JSONSchemaFilePath = json_file_path
	// HTTP path where to make json schemas available
	controller_opts.JSONSchemaRoutePath = "/schemas"
	// If set, where output for apache-style logging goes
	controller_opts.ApacheLogWriter = os.Stderr
	// Set the request trace manager
	controller_opts.RequestTraceManager = req_trace_manager

	controller := api_framework.NewController(controller_opts)

	// Need a context to start
	ctx := context.Background()

	if err := controller.Init(ctx); err != nil {
		logger.LogError(ctx, err)
		panic(err)
	}

	if err := registerKittens(controller); err != nil {
		logger.LogError(ctx, err)
		panic(err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("Server started on port %d", port))

	err = http.ListenAndServe(
		fmt.Sprintf(":%d", port),
		controller,
	)

	logger.LogError(ctx, err)
	panic(err)
}
