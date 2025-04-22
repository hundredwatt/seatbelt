# PySeatbelt

A Python library for data validation between different sources and targets.

## Features

- Validate data integrity between source and target systems
- Abstract Source and Target interfaces for easy implementation
- Track data changes and validation errors
- Detailed metrics on validation status

## Installation

```bash
# Install from the repository
pip install -e .
```

## Usage

PySeatbelt provides abstract `Source` and `Target` classes that you can implement to connect to your data stores. The `ValidationEngine` handles the validation logic.

### Basic Example

```python
from pyseatbelt import Source, Target, ValidationEngine

# Implement your source and target
class MySource(Source):
    def read_change_log_changes(self, column_names):
        # Implement reading change log entries
        return {}
        
    def read_signatures(self, column_names):
        # Implement reading signatures
        return {}

class MyTarget(Target):
    def read_signatures(self, column_names):
        # Implement reading signatures
        return {}

# Create validation engine
engine = ValidationEngine()

# Run validation
metrics = engine.seatbelt_check(MySource(), MyTarget())

# Check results
print(f"Valid: {metrics['valid_count']}")
print(f"Pending: {metrics['pending_count']}")
print(f"Errors: {metrics['error_count']}")
```

See the `examples` directory for more detailed examples.

## Testing

Run the unit tests using:

```bash
python -m unittest discover tests
```

## License

MIT 