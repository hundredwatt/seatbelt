# Data Replication TUI Simulator

A Text-based User Interface for simulating a two-stage data replication process.

## Overview

This simulator visualizes the data flow from a source database to a target database through a two-stage replication pipeline. It shows operations on the source database (INSERT, UPDATE, DELETE) and the replication process (EXTRACT, LOAD).

## Features

- Source DB display (up to 8 rows)
- Target DB display with highlighted recent changes
- 2-stage replication pipeline visualization
- Real-time metrics including lag and operation counts
- Log viewer
- Interactive keyboard controls

## Installation

1. Install the required dependencies:

```
pip install -r requirements.txt
```

## Running the Simulator

```
python tui.py
```

## Keyboard Controls

- `i`: Insert a new row into the source database
- `u`: Update a random row in the source database
- `d`: Delete a random row from the source database
- `e`: Extract data from source into staging area
- `l`: Load data from staging area to target database
- `q`: Quit the simulator

## Interface Layout

```
+----------------+----------------+----------------+
|   Source DB    |    Pipeline    |   Target DB    |
|                |                |                |
|                |                |                |
|                |                |                |
|                |                |                |
+----------------+----------------+----------------+
|                    Logs                |  Metrics |
|                                        |          |
|                                        |          |
|                                        |          |
|                                        |          |
+----------------------------------------+----------+
```

## How It Works

1. Operations on the source database are simulated (insert, update, delete)
2. The extract phase creates a snapshot of the source database in a staging area
3. The load phase moves data from the staging area to the target database
4. Lag metrics show how many operations have not yet been synchronized

## Note

This is a simplified simulation for educational purposes and does not implement all features of a real data replication system. 