# Contributing to FocusStreamer

Thank you for your interest in contributing to FocusStreamer! This document provides guidelines and instructions for contributing.

## Development Setup

### Prerequisites

- Go 1.21 or later
- Node.js 18 or later
- X11 development libraries
- Git

### Installation

1. Fork and clone the repository:
```bash
git clone https://github.com/yourusername/FocusStreamer.git
cd FocusStreamer
```

2. Install Go dependencies:
```bash
make install-deps
```

3. Install frontend dependencies:
```bash
cd web
npm install
```

## Development Workflow

### Backend Development

```bash
# Run backend in development mode
make dev-backend

# Run tests
make test

# Build binary
make build-backend
```

### Frontend Development

```bash
# Run frontend dev server (with hot reload)
cd web
npm run dev
```

The frontend will be available at `http://localhost:3000` and will proxy API requests to `http://localhost:8080`.

### Running Both Together

```bash
# Terminal 1: Run backend
make dev-backend

# Terminal 2: Run frontend
cd web
npm run dev
```

## Code Style

### Go

- Follow standard Go conventions
- Use `gofmt` for formatting
- Run `go vet` before committing
- Add comments for exported functions and types

### JavaScript/React

- Use functional components with hooks
- Follow React best practices
- Keep components small and focused
- Use meaningful variable names

## Project Structure

```
FocusStreamer/
├── cmd/
│   └── server/         # Main application entry point
├── internal/
│   ├── api/           # HTTP API server
│   ├── config/        # Configuration management
│   ├── window/        # X11 window management
│   └── display/       # Virtual display (future)
├── web/
│   └── src/
│       ├── components/ # React components
│       ├── hooks/      # Custom React hooks
│       └── utils/      # Utility functions
└── pkg/               # Public libraries (if any)
```

## Adding New Features

1. Create a feature branch:
```bash
git checkout -b feature/your-feature-name
```

2. Make your changes following the code style guidelines

3. Add tests for new functionality

4. Update documentation as needed

5. Commit your changes:
```bash
git add .
git commit -m "feat: add your feature description"
```

6. Push and create a pull request:
```bash
git push origin feature/your-feature-name
```

## Commit Message Guidelines

We follow conventional commits:

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `style:` - Code style changes (formatting, etc.)
- `refactor:` - Code refactoring
- `test:` - Adding or updating tests
- `chore:` - Maintenance tasks

Examples:
```
feat: add pattern matching for window classes
fix: resolve race condition in window manager
docs: update README with installation instructions
```

## Testing

### Backend Tests

```bash
# Run all tests
make test

# Run specific package tests
go test ./internal/window -v
```

### Frontend Tests

```bash
cd web
npm test
```

## Pull Request Process

1. Ensure your code builds and tests pass
2. Update README.md or other docs if needed
3. Add a clear description of changes in the PR
4. Link any related issues
5. Wait for review and address feedback

## Code Review Guidelines

Reviewers will check for:

- Code quality and style
- Test coverage
- Documentation
- Performance implications
- Security considerations

## Getting Help

- Open an issue for bugs or feature requests
- Join discussions in existing issues
- Ask questions in pull request comments

## License

By contributing, you agree that your contributions will be licensed under the same license as the project (see LICENSE.md).
