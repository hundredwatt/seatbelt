#!/usr/bin/env python
"""
Seatbelt Demo - Terminal UI and simulator for data validation demo
"""

from setuptools import setup, find_packages

setup(
    name="seatbelt-demo",
    version="0.1.0",
    description="A terminal UI and simulator for demonstrating Seatbelt data validation",
    author="SeatbeltData",
    author_email="info@seatbeltdata.com",
    url="https://github.com/seatbeltdata/demo-tui",
    packages=find_packages(),
    entry_points={
        "console_scripts": [
            "seatbelt-tui=seatbelt_demo.ui.tui:run_tui",
        ],
    },
    install_requires=[
        "faker>=8.0.0",
    ],
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Environment :: Console :: Curses",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Operating System :: OS Independent",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.9",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
        "Topic :: Database :: Database Engines/Servers",
        "Topic :: Software Development :: Testing",
        "Topic :: System :: Monitoring",
    ],
    python_requires=">=3.9",
) 