module top (input clk, input [3:0] d, output logic [3:0] q);
  logic [3:0] r, s, t;
  logic _scratch, unused_flag, dummy_x;
  wire w = d[0];
  logic [3:0] arr [2];
  logic [3:0] idx;
  struct packed { logic a; logic b; } st;
  event ev;
  always_ff @(posedge clk) begin
    r <= d;
    arr[idx[0]] <= r;
    st.a <= w;
    -> ev;
  end
  always_comb begin s = r; t = s; end
  always @(ev) q <= t | arr[0] | {3'b0, st.b};
  leaf u(.d(w), .q());
endmodule
module leaf(input d, output q); assign q = d; endmodule
