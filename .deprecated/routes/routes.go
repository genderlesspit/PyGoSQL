package routes

import (
	"gosql/models" // matches your module name
)

type Route struct {
	Path   string
	Method models.Method
}

func MethodsToRoutes(methods []models.Method) []Route {
	var routes []Route

	for _, m := range methods {
		var path string

		if m.IsUniversal() {
			path = "/" + m.GetName()
		} else if tm, ok := m.(models.TableMethods); ok {
			path = "/" + tm.Table + "/" + tm.Name
		} else {
			continue
		}

		routes = append(routes, Route{
			Path:   path,
			Method: m,
		})
	}

	return routes
}

// Now uses models.Setup() to do all the heavy lifting
func Run() ([]Route, error) {
	methods, err := models.Setup("gosql_dir/db")
	if err != nil {
		return nil, err
	}
	return MethodsToRoutes(methods), nil
}