from setuptools import setup, find_packages

setup(
    name="pyseatbelt",
    version="0.1.0",
    description="Data validation library for ensuring data integrity between sources and targets",
    author="Seatbelt Data Team",
    author_email="info@seatbeltdata.com",
    packages=find_packages(),
    python_requires=">=3.7",
    install_requires=[
        # No external dependencies currently required
    ],
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.7",
        "Programming Language :: Python :: 3.8",
        "Programming Language :: Python :: 3.9",
    ],
)
