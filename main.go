package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/tidwall/buntdb"
)

// A single todo item
type Todo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"createdAt"`
}

// TodoIDRequest is used for operations that require only a todo ID
type TodoIDRequest struct {
	ID string `json:"id" validate:"required"`
}

var (
	db       *buntdb.DB
	template *JTemplate
)

const (
	MaxTodos = 150
)

func main() {
	// Initialize database
	var err error
	db, err = buntdb.Open("data.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create static directory if it doesn't exist
	if err = os.MkdirAll("./static", 0755); err != nil {
		log.Fatalf("Failed to create static directory: %v", err)
	}

	// Initialize and download required libraries
	libsMap, err := EnsureStaticLibs("./static", AlpineJS, TailwindCSS, AlpineAutoAnimate)
	if err != nil {
		log.Fatalf("Failed to ensure static libraries: %v", err)
	}

	// Load and prepare the template
	template, err = NewJTemplate("index.html", libsMap)
	if err != nil {
		log.Fatalf("Failed to create template: %v", err)
	}

	// Set up routes
	router := mux.NewRouter()
	router.HandleFunc("/", handleIndex).Methods("GET")
	router.HandleFunc("/todos", handleGetTodos).Methods("GET")
	router.HandleFunc("/todos", handleCreateTodo).Methods("POST")
	router.HandleFunc("/todos/toggle", handleToggleTodo).Methods("POST")
	router.HandleFunc("/todos/delete", handleDeleteTodo).Methods("POST")
	router.HandleFunc("/todos/clear-completed", handleClearCompleted).Methods("POST")

	// Serve static files
	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))),
	)

	// Start the server
	log.Println("Server starting on http://localhost:8080")
	if err := http.ListenAndServe(":8080", router); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// handleIndex serves the main page
func handleIndex(w http.ResponseWriter, r *http.Request) {
	todos, err := getAllTodos()
	if err != nil {
		http.Error(w, "Failed to fetch todos", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"todoApp::todos":   todos,
		"todoApp::newTodo": "",
		"todoApp::filter":  "all",
	}

	if err := template.Execute(w, data); err != nil {
		log.Printf("Error rendering template: %v", err)
	}
}

// handleGetTodos handles GET requests for todos
func handleGetTodos(w http.ResponseWriter, r *http.Request) {
	todos, err := getAllTodos()
	if err != nil {
		template.Error(w, "Failed to fetch todos")
		return
	}
	template.JSON(w, map[string]interface{}{
		"todoApp::todos": todos,
	})
}

// handleCreateTodo handles POST requests to create a new todo
func handleCreateTodo(w http.ResponseWriter, r *http.Request) {
	type NewTodoRequest struct {
		Text string `json:"newTodo" validate:"required,min=1,max=100"`
	}

	req, ok := DecodeAndValidate[NewTodoRequest](template, w, r)
	if !ok {
		return
	}

	// Check if we've reached the maximum number of todos
	todos, err := getAllTodos()
	if err != nil {
		template.Error(w, "Failed to check todos count")
		return
	}

	if len(todos) >= MaxTodos {
		template.Error(w, fmt.Sprintf("Maximum number of todos (%d) reached. Please delete some todos first.", MaxTodos))
		return
	}

	// Create and save new todo
	todo := Todo{
		ID:        uuid.New().String(),
		Text:      req.Text,
		Completed: false,
		CreatedAt: time.Now(),
	}

	if err := saveTodo(todo); err != nil {
		template.Error(w, "Failed to save todo")
		return
	}

	// Return updated list
	todos, err = getAllTodos()
	if err != nil {
		template.Error(w, "Failed to fetch updated todos")
		return
	}

	template.JSON(w, map[string]interface{}{
		"todoApp::todos":   todos,
		"todoApp::newTodo": "", // Clear the input field
		"main::error":      "", // Clear error
	})
}

// handleToggleTodo toggles the completed status of a todo
func handleToggleTodo(w http.ResponseWriter, r *http.Request) {
	req, ok := DecodeAndValidate[TodoIDRequest](template, w, r)
	if !ok {
		return
	}

	// Find and toggle the todo
	err := db.Update(func(tx *buntdb.Tx) error {
		val, err := tx.Get("todo:" + req.ID)
		if err != nil {
			return err
		}

		var todo Todo
		if err := json.Unmarshal([]byte(val), &todo); err != nil {
			return err
		}

		// Toggle the completed status
		todo.Completed = !todo.Completed

		// Save the updated todo
		todoJSON, err := json.Marshal(todo)
		if err != nil {
			return err
		}
		_, _, err = tx.Set("todo:"+todo.ID, string(todoJSON), nil)
		return err
	})

	if err != nil {
		template.Error(w, "Failed to toggle todo: "+err.Error())
		return
	}

	// Return updated list
	todos, err := getAllTodos()
	if err != nil {
		template.Error(w, "Failed to fetch updated todos: "+err.Error())
		return
	}

	template.JSON(w, map[string]interface{}{
		"todoApp::todos": todos,
	})
}

// handleDeleteTodo deletes a todo
func handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	req, ok := DecodeAndValidate[TodoIDRequest](template, w, r)
	if !ok {
		return
	}

	// Delete the todo
	err := db.Update(func(tx *buntdb.Tx) error {
		_, err := tx.Delete("todo:" + req.ID)
		return err
	})

	if err != nil && err != buntdb.ErrNotFound {
		template.Error(w, "Failed to delete todo: "+err.Error())
		return
	}

	// Return updated list
	todos, err := getAllTodos()
	if err != nil {
		template.Error(w, "Failed to fetch updated todos: "+err.Error())
		return
	}

	template.JSON(w, map[string]interface{}{
		"todoApp::todos": todos,
	})
}

// handleClearCompleted removes all completed todos
func handleClearCompleted(w http.ResponseWriter, r *http.Request) {
	// Get all todos
	todos, err := getAllTodos()
	if err != nil {
		template.Error(w, "Failed to fetch todos: "+err.Error())
		return
	}

	// Delete all completed todos
	err = db.Update(func(tx *buntdb.Tx) error {
		for _, todo := range todos {
			if todo.Completed {
				_, err := tx.Delete("todo:" + todo.ID)
				if err != nil && err != buntdb.ErrNotFound {
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		template.Error(w, "Failed to clear completed todos: "+err.Error())
		return
	}

	// Return updated list
	todos, err = getAllTodos()
	if err != nil {
		template.Error(w, "Failed to fetch updated todos: "+err.Error())
		return
	}

	template.JSON(w, map[string]interface{}{
		"todoApp::todos": todos,
	})
}

// saveTodo stores a todo in the database
func saveTodo(todo Todo) error {
	return db.Update(func(tx *buntdb.Tx) error {
		todoJSON, err := json.Marshal(todo)
		if err != nil {
			return err
		}
		_, _, err = tx.Set("todo:"+todo.ID, string(todoJSON), nil)
		return err
	})
}

// getAllTodos retrieves all todos from the database
func getAllTodos() ([]Todo, error) {
	todos := make([]Todo, 0)

	err := db.View(func(tx *buntdb.Tx) error {
		return tx.Ascend("", func(key, value string) bool {
			// Only process todo items (keys starting with "todo:")
			if len(key) > 5 && key[:5] == "todo:" {
				var todo Todo
				if err := json.Unmarshal([]byte(value), &todo); err != nil {
					// Skip this item on error
					return true
				}
				todos = append(todos, todo)
			}
			return true // Continue iteration
		})
	})

	return todos, err
}
