"""Metrics tracking for the Seatbelt Demo simulator."""

class MetricsTracker:
    """Class responsible for tracking and reporting metrics"""
    
    def __init__(self):
        self.metrics = {
            "lag": 0,
            "source_ops_count": 0,
            "target_ops_count": 0,
            "corruption_count": 0,
            "source_db_size": 0,
            "target_db_size": 0,
            "seatbelt_size": 0,
            "error_count": 0,
            "pending_count": 0,
            "valid_count": 0,
        }
    
    def update(self, **kwargs):
        """Update multiple metrics at once"""
        self.metrics.update(kwargs)
    
    def increment(self, key, value=1):
        """Increment a specific metric"""
        self.metrics[key] += value
    
    def set(self, key, value):
        """Set a specific metric"""
        self.metrics[key] = value
    
    def get(self, key=None):
        """Get a specific metric or all metrics"""
        if key:
            return self.metrics.get(key, 0)
        return self.metrics
    
    def calculate_lag(self, source_sequence_no, sync_state):
        """Calculate and update the lag metric"""
        if sync_state['last_load_ts'] == -1:
            self.metrics["lag"] = source_sequence_no
        else:
            self.metrics["lag"] = source_sequence_no - sync_state['last_load_ts'] 