# Seatbelt Demo TUI

A terminal user interface (TUI) for demonstrating the Seatbelt data validation framework. This tool simulates a data replication pipeline with optional corruption scenarios to illustrate how Seatbelt detects data discrepancies.

## Overview

This project provides:

1. **A terminal UI** for interactive demonstrations of Seatbelt's capabilities
2. **A simulation backend** that can be used programmatically for automated testing or custom scenarios
3. **Example code** showing how to use the simulator in different contexts
4. **Customizable data schemas** allowing you to define your own table structure and data types
5. **Testing framework** for creating and running tests using the configuration file format

## Installation

### Development Setup

```bash
# Clone the repository
git clone https://github.com/seatbeltdata/demo-tui.git
cd demo-tui

# Create and activate a virtual environment (optional but recommended)
python -m venv .venv
source .venv/bin/activate  # On Windows: .venv\Scripts\activate

# Install dependencies
pip install -e .
```

### Platform-Specific Notes

#### Windows
- Use `py -m venv .venv` to create the virtual environment
- Activate with `.venv\Scripts\activate`
- The Windows-specific curses package is automatically installed via requirements.txt

#### macOS
- You may need to install Python 3 via Homebrew if not already installed: `brew install python`
- Make sure you have the latest pip: `python -m pip install --upgrade pip`

#### Linux
- Make sure you have Python 3.9+ and development packages installed
- On Debian/Ubuntu: `sudo apt-get install python3-dev python3-venv`
- On Red Hat/Fedora: `sudo dnf install python3-devel`

## Usage

### Running the TUI Demo

```bash
# Run directly from Python
python -m seatbelt_demo.ui.tui

# Or use the console script
seatbelt-tui

# With a custom schema configuration
seatbelt-tui --config seatbelt_demo/configs/customer_data_example.yaml
```

### Using the Simulator Programmatically

```python
from seatbelt_demo.simulator import Simulator
from seatbelt_demo.simulator.database import SchemaDefinition, ColumnDefinition, ColumnType, InitialData

# Default schema (name and score columns)
simulator = Simulator()

# Or with a custom schema
schema = SchemaDefinition()
schema.add_column(ColumnDefinition(
    name='email', 
    type=ColumnType.STRING,
    nullable=False
))
schema.add_column(ColumnDefinition(
    name='age',
    type=ColumnType.INTEGER,
    nullable=True,
    # Optional conversion to float in target
    target_type=ColumnType.FLOAT
))

# Initial data configuration
initial_data = InitialData(row_count=5)  # Generate 5 random rows
# Or add specific rows
initial_data.add_row({
    'email': 'user@example.com',
    'age': 30
})

# Initialize with custom schema
simulator = Simulator(random_seed=42, schema=schema, initial_data=initial_data)

# Or load from a configuration file
simulator = Simulator.from_config_file('config.yaml')

# Perform operations
simulator.insert_row()
simulator.update_row()
simulator.extract()
simulator.load()

# Check for validation errors
simulator.seatbelt_check()

# Get metrics
metrics = simulator.metrics_tracker.get()
print(f"Metrics: {metrics}")
```

## Customizable Schema

The simulator now supports fully customizable data schemas with the following features:

- Define any number of columns with different data types
- Support for common data types: string, integer, float, boolean, date, datetime
- Configure nullable columns
- Type transformations between source and target (e.g., INTEGER to FLOAT)
- Custom data generators for realistic test data
- Initial data configuration for testing specific scenarios

### Configuration Files

Schemas and simulation plans can be defined in YAML or JSON configuration files:

```yaml
# Example config.yaml
random_seed: 42
seatbelt_interval: 25

schema:
  columns:
    - name: first_name
      type: string
      nullable: false
    - name: age
      type: integer
      nullable: true
      target_type: float
    - name: is_active
      type: boolean
      nullable: false

initial_data:
  row_count: 5
  rows:
    - first_name: John
      age: 30
      is_active: true
    
# Define a simulation plan
plan:
  - operation: insert
  - operation: extract
  - operation: load
```

See `seatbelt_demo/configs/customer_data_example.yaml` for a complete example.

## Testing Framework

The project includes a testing framework that uses the config file format to create and run tests.

### Creating Tests

Tests are defined using the same YAML configuration format with added expectations:

```yaml
# Example test.yaml
name: Sample Test
random_seed: 42
description: Test for validation logic

# Define global expectations
expect:
  - name: No validation errors
    type: validation_error
    expected: false
  
  - name: Row count matches
    type: metric
    metric: source_db_size
    comparison: equal
    value: 5

# Define test operations
plan:
  - operation: insert
  - operation: insert
  - operation: extract
  - operation: load
  - operation: seatbelt_check
  
  # In-plan expectations
  - operation: expect
    name: Target matches source
    type: metric
    metric: target_db_size
    comparison: equal
    value: 2
```

### Supported Expectations

The testing framework supports different types of expectations:

1. **Metric expectations** - Check simulator metrics
   ```yaml
   - name: Row count
     type: metric
     metric: source_db_size
     comparison: equal  # equal, not_equal, greater_than, less_than, greater_or_equal, less_or_equal
     value: 5
   ```

2. **Validation error expectations** - Check for validation errors
   ```yaml
   - name: No errors
     type: validation_error
     expected: false  # true = errors expected, false = no errors expected
   ```

3. **Row existence expectations** - Check if a row exists
   ```yaml
   - name: Row exists
     type: row_exists
     id: 1
     target: source  # source or target
     exists: true
   ```

### Running Tests

Run tests using the provided script:

```bash
# Run all tests in the tests directory
./run_tests.py

# Run specific test files
./run_tests.py tests/basic_validation_test.yaml tests/corrupt_target_test.yaml

# Run with verbose output
./run_tests.py -v
```

## TUI Controls

The TUI provides keyboard controls for interacting with the simulation:

- `i`: Insert a new row
- `u`: Update a random row
- `d`: Delete a random row
- `I`: Insert a row with NULL score
- `U`: Update a row with NULL score
- `e`: Extract data from source to staging
- `l`: Load data from staging to target
- `s`: Run seatbelt validation check
- `r`: Remove a random ID from the corruption filter
- `n`: Toggle NULL corruption (NULL mismap)
- `^i` (Ctrl+i): Corrupt by insert
- `^u` (Ctrl+u): Corrupt by update
- `^x` (Ctrl+x): Corrupt target score
- `q`: Quit the application

## Project Structure

```
seatbelt_demo/
├── __init__.py              # Package initialization
├── configs/                 # Example configurations
│   └── customer_data_example.yaml # Example schema configuration
├── simulator/               # Simulation backend
│   ├── __init__.py
│   ├── database.py          # Database operations with customizable schema
│   ├── config.py            # Configuration loading system
│   ├── corruptor.py         # Corruption logic
│   ├── etl.py               # Extract-Transform-Load operations
│   ├── metrics.py           # Metrics tracking
│   ├── simulator.py         # Main simulator class
│   └── validation.py        # Validation engine
├── validation/              # Validation logic
│   ├── __init__.py
│   └── logic.py             # Core validation algorithms
└── ui/                      # User interface
    ├── __init__.py
    └── tui.py               # Terminal UI implementation
```

## Examples

See the `examples/` directory for sample code showing how to use the simulator:

- `basic_simulation.py` - Simple simulation without the TUI
- `custom_schema_simulation.py` - Example with customized schema and data types

## Troubleshooting

### Common Issues

1. **Terminal size too small**: 
   - The TUI requires a minimum terminal size to display properly.
   - Increase your terminal window size or reduce font size.

2. **Colors not displaying**:
   - Make sure your terminal supports colors.
   - Try setting the TERM environment variable: `export TERM=xterm-256color`

3. **Windows-specific issues**:
   - If seeing `ImportError: No module named _curses`, ensure you've installed the windows-curses package.
   - For Unicode issues, try: `chcp 65001` in your terminal before running.

4. **Keyboard input not working**:
   - Some terminal emulators capture certain key combinations. Try alternative keys.
   - Verify your terminal is properly passing key events to the application.

### Error Reporting

If you encounter bugs:
1. Run with the `--check-only` flag to verify initialization works
2. Check the `tui_debug.log` file for error messages
3. Open an issue on GitHub with the complete error logs

