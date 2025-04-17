#!/usr/bin/env python3
"""
Basic Simulation Example for Seatbelt Demo

This example demonstrates how to use the Seatbelt Demo simulator
without the TUI interface, useful for automated testing or batch simulations.
"""

import logging
import argparse
import time
from seatbelt_demo.simulator import Simulator

# Configure logging to output to console
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)

def parse_args():
    """Parse command-line arguments."""
    parser = argparse.ArgumentParser(description='Seatbelt Demo Basic Simulation')
    parser.add_argument('--seed', type=int, default=42, help='Random seed for reproducibility')
    parser.add_argument('--pause', type=float, default=0.1, help='Pause between operations (seconds)')
    parser.add_argument('--corrupt', action='store_true', help='Include corruption scenarios')
    return parser.parse_args()

def run_basic_simulation(seed=42, pause=0.1, include_corruption=False):
    """Run a basic simulation with the specified parameters."""

    # Initialize the simulator
    simulator = Simulator(random_seed=seed)
    logging.info("Simulator initialized with seed %d", seed)

    # Create a simulation plan
    plan = create_simulation_plan(include_corruption)

    # Execute the plan with pauses
    for i, step in enumerate(plan):
        logging.info(f"Step {i+1}/{len(plan)}: Executing {step.__name__}")
        step()
        time.sleep(pause)

    # Print final metrics
    metrics = simulator.metrics_tracker.get()
    logging.info("Simulation completed.")
    logging.info(f"Final metrics: {metrics}")

    return simulator

def create_simulation_plan(include_corruption=False):
    """Create a list of simulation steps."""
    simulator = Simulator()

    # Basic plan without corruption
    plan = []

    # Initial data setup
    for _ in range(5):
        plan.append(simulator.insert_row)

    # First extract-load cycle
    plan.append(simulator.extract)
    plan.append(simulator.load)

    # Add some NULL values
    plan.append(simulator.insert_with_null)
    plan.append(simulator.update_with_null)

    # More data modifications
    for _ in range(3):
        plan.append(simulator.update_row)
    plan.append(simulator.delete_row)

    # Another extract-load cycle
    plan.append(simulator.extract)
    plan.append(simulator.load)

    # Add corruption if requested
    if include_corruption:
        plan.append(simulator.corrupt_by_insert)
        plan.append(simulator.corrupt_by_update)
        plan.append(simulator.toggle_null_corruption)
        plan.append(simulator.extract)
        plan.append(simulator.load)
        plan.append(simulator.corrupt_target_score)

    # Run validation
    plan.append(simulator.seatbelt_check)

    # Final operations
    for _ in range(2):
        plan.append(simulator.random_operation)

    # Final extract-load-validate cycle
    plan.append(simulator.extract)
    plan.append(simulator.load)
    plan.append(simulator.seatbelt_check)

    return plan

if __name__ == "__main__":
    args = parse_args()
    run_basic_simulation(args.seed, args.pause, args.corrupt)
