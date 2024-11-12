package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"reflect"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

var (
	contextPackage    = protogen.GoImportPath("context")
	grpcPackage       = protogen.GoImportPath("google.golang.org/grpc")
	runtimePackage    = protogen.GoImportPath("github.com/grpc-ecosystem/grpc-gateway/v2/runtime")
	middlewarePackage = protogen.GoImportPath("github.com/grpc-ecosystem/go-grpc-middleware")

	useXGRPC        = flag.Bool("use-xgrpc", false, "Use xgrpc-go")
	registerGateway = flag.Bool("register-gateway", true, "enable register handler servers in RegisterGateway")
)

// NOTE: do not forget to change this version with each new tag, this is very important
const version = "v0.0.1"

func main() {
	var flags flag.FlagSet

	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			generateFile(gen, f)
		}
		return nil
	})
}

// generateFile generates a _ascii.pb.go file containing gRPC service definitions.
func generateFile(gen *protogen.Plugin, file *protogen.File) *protogen.GeneratedFile {

	var (
		service   = file.Services[0]
		descName  = service.GoName + "ServiceDesc"
		proxyName = "proxy" + service.GoName + "Server"
	)

	// Генерируем  файл
	g := gen.NewGeneratedFile(file.GeneratedFilenamePrefix+".pb.etk3.go", file.GoImportPath)
	g.P("// Code generated by protoc-gen-etk3. DO NOT EDIT.")
	g.P("// versions:")
	protocVersion := "(unknown)"
	if v := gen.Request.GetCompilerVersion(); v != nil {
		protocVersion = fmt.Sprintf("v%v.%v.%v", v.GetMajor(), v.GetMinor(), v.GetPatch())
	}

	g.P("// \tprotoc-gen-etk3: ", version)
	g.P("// \tprotoc:             ", protocVersion)
	if file.Proto != nil && file.Proto.Name != nil {
		g.P("// source: ", *file.Proto.Name)
	}
	// go_package из прото-файла
	g.P("package ", file.GoPackageName)

	// Go file with go:embed must import "embed" package
	g.P()
	g.P(`import _ "embed"`)
	g.P()
	// Встраиваем содержимое swagger.json в swaggerJson, имя берем по названию прото-файла
	g.P("//go:embed ", trimPathAndExt(file.Proto.GetName()), `.swagger.json`)
	g.P("var swaggerJSON []byte")

	// Объявление типа ServiceDesc
	g.P("type ", descName, " struct {")
	// GRPC-server, прокидывается из имплементации
	g.P("svc ", service.GoName, "Server")
	// Interceptor
	g.P("i ", g.QualifiedGoIdent(grpcPackage.Ident("UnaryServerInterceptor")))
	g.P("}")
	g.P()

	// Конструктор ServiceDesc
	g.P("func New", descName, "(i ", service.GoName, "Server) ", "*", descName, " {")
	g.P("return &", descName, "{svc: i}")
	g.P("}")
	g.P()

	// Геттер swagger.json
	g.P("func(d *", descName, ") SwaggerDef() []byte {")
	g.P(`return swaggerJSON`)
	g.P("}")
	g.P()
	g.P("func(d *", descName, ") RegisterGRPC(s *", g.QualifiedGoIdent(grpcPackage.Ident("Server")), ") {")
	if *useXGRPC {
		g.P("Register", service.GoName, "XGRPCServer(s, d.svc)")
	} else {
		g.P("Register", service.GoName, "Server(s, d.svc)")
	}

	g.P("}")
	g.P()

	g.P("func(d *", descName, ") RegisterGateway(ctx ", g.QualifiedGoIdent(contextPackage.Ident("Context")), ", mux *", g.QualifiedGoIdent(runtimePackage.Ident("ServeMux")), ") error {")

	if *registerGateway || methodsHaveHttpOptions(service.Methods) {
		g.P("if d.i == nil {")
		g.P("return Register", service.GoName, "HandlerServer(ctx, mux, d.svc)")
		g.P("}")
		g.P("return Register", service.GoName, "HandlerServer(ctx, mux, &", proxyName, "{")
		g.P(service.GoName, "Server: d.svc,")
		g.P("interceptor: d.i,")
		g.P("})")
	} else {
		g.P("return nil")
	}
	g.P("}")
	g.P()

	g.P("func(d *", descName, ") WithHTTPUnaryInterceptor(u ", g.QualifiedGoIdent(grpcPackage.Ident("UnaryServerInterceptor")), ") {")
	g.P("if d.i == nil {")
	g.P("d.i = u")
	g.P("} else {")
	g.P("d.i = ", g.QualifiedGoIdent(middlewarePackage.Ident("ChainUnaryServer")), "(d.i, u)")
	g.P("}")
	g.P("}")
	g.P()

	g.P("type ", proxyName, " struct {")
	g.P(service.GoName, "Server")
	g.P("interceptor ", g.QualifiedGoIdent(grpcPackage.Ident("UnaryServerInterceptor")))
	g.P("}")
	g.P()

	//for _, method := range service.Methods {
	//	// Create proxy method for GRPC methods with interceptors
	//	// Condition is the same as in grpc-go plugin:
	//	// https://github.com/grpc/grpc-go/blob/6351a55c3895e5658b2c59769c81109d962d0e04/cmd/protoc-gen-go-grpc/grpc.go#L370
	//	if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
	//		g.P("func (p *", proxyName, ") ", method.GoName, "(ctx ", contextPackage.Ident("Context"), ", req *", method.Input.GoIdent, ") (*", method.Output.GoIdent, ", error) {")
	//		g.P("info := &", grpcPackage.Ident("UnaryServerInfo"), "{")
	//		g.P("Server: p.", service.GoName, "Server,")
	//		g.P("FullMethod: ", strconv.Quote(fmt.Sprintf("/%s/%s", service.Desc.FullName(), method.Desc.Name())), ",")
	//		g.P("}")
	//
	//		g.P("handler := func(ctx ", contextPackage.Ident("Context"), ", req interface{}) (interface{}, error) {")
	//		g.P("return p.", service.GoName, "Server.", method.GoName, "(ctx, req.(*", method.Input.GoIdent, "))")
	//		g.P("}")
	//		g.P("resp, err := p.interceptor(ctx, req, info, handler)")
	//		g.P("if err != nil || resp == nil {")
	//		g.P("return nil, err")
	//		g.P("}")
	//		g.P("return resp.(*", method.Output.GoIdent, "), err")
	//		g.P("}")
	//		g.P()
	//	}
	//}

	return g
}

func trimPathAndExt(fName string) string {
	f := filepath.Base(fName)
	ext := filepath.Ext(f)
	return f[:len(f)-len(ext)]
}

// Проверяет наличие опции api.http у методов
// Работает через проверку наличия extension, по аналогии grpc-gateway
// https://github.com/grpc-ecosystem/grpc-gateway/blob/main/internal/descriptor/services.go#L201
func methodsHaveHttpOptions(methods []*protogen.Method) bool {
	for _, method := range methods {
		ext := proto.GetExtension(method.Desc.Options(), annotations.E_Http)
		// Возвращается interface{}, поэтому прямая проверка на nil не работает.
		if reflect.ValueOf(ext).IsNil() {
			continue
		}
		if _, ok := ext.(*annotations.HttpRule); ok {
			return true
		}
	}

	return false
}
