# JAlpine: A Go + Alpine.js Micro-Framework

> [!IMPORTANT]
> **Proof of Concept: Alternative Alpine.js Usage**  
> 
> This project demonstrates an **experimental approach** to Alpine.js usage. While Alpine.js was designed primarily for enhancing server-rendered HTML (as used with Laravel Livewire or go templ), JAlpine repurposes it as a SPA framework with JSON API communication.
>
> This architectural pattern diverges from Alpine's intended use case but explores its potential for lightweight Go-backed SPAs without complex frontend tooling. Consider it an interesting experiment rather than a production-ready solution.
>
> *This README was generated with Anthropic Claude 3.7 Sonnet.* Sorry for that

JAlpine is a lightweight micro-framework that seamlessly integrates Go on the backend with Alpine.js on the frontend. It provides a simple yet powerful way to build interactive web applications with minimal boilerplate.

DEMO: https://alpine.jipok.ru/

## Features

- **Zero-build frontend** - No Node.js, webpack, or complex build setup required
- **Server-side template handling** with dynamic includes and hot-reloading
- **Automatic dependency management** for external libraries (Alpine.js, Tailwind CSS)
- **Component-namespaced data binding** between server and client
- **Integrated AJAX helpers** via Alpine.js magic methods
- **Form validation** using go-playground/validator
- **Hot code reload detection** with automatic client refresh

## How It Works

JAlpine brings together the simplicity of Alpine.js with the performance of Go:

1. **Backend**: Go handles routing, data persistence, and HTML template rendering
2. **Frontend**: Alpine.js provides reactive data binding and DOM manipulation
3. **Integration**: JTemplate connects the two worlds with automatic data synchronization

The framework handles the complexities of:

- Downloading and managing frontend dependencies
- Injecting scripts and styles into HTML
- Processing template includes and component definitions
- Synchronizing component state between server and client

## Demo Todo Application

The included Todo app demonstrates JAlpine's capabilities:

- Create, toggle, and delete todos
- Filter todos by status (all, active, completed)
- Clear completed todos
- Client-side validation
- Real-time UI updates without page reloads
- Automatic version checking for hot reloads

## Getting Started

### Prerequisites

- Go 1.18 or higher
- Internet connection (for initial dependency download)

### Installation

1. Clone this repository:
   ```
   git clone https://github.com/yourusername/jalpine.git
   cd jalpine
   ```

2. Run the application:
   ```
   go run *.go
   ```

3. Open your browser at [http://localhost:8080](http://localhost:8080)

## Technical Details

### Core Components

#### JTemplate

The template engine handles:
- Dynamic file includes (`<% include file %>`)
- Source mapping for debugging
- Alpine.js component integration
- Version tracking for hot reloads

#### Static Library Management

Automatically downloads and manages:
- Alpine.js core and plugins
- Tailwind CSS
- Other frontend dependencies

#### AJAX Integration

Provides seamless API communication through Alpine.js magic methods:
- `$get(url)`
- `$post(url, data)`
- `$put(url, data)`
- `$patch(url, data)`
- `$delete(url, data)`

### Directory Structure

```
├── static/              # Auto-generated frontend dependencies
├── index.html           # Main template
├── main.go              # Application entrypoint and routes
├── template.go          # Template engine implementation
├── helpers.js           # Client-side helpers
└── data.db              # BuntDB database file (auto-created)
```

## Why Use JAlpine?

JAlpine is perfect for:

- Small to medium web applications
- Projects that need rapid development
- Teams that prefer Go on the backend
- Developers who want Alpine.js simplicity without complex frontend tooling

## License

MIT

## Contributing

Contributions welcome! Please feel free to submit a Pull Request.