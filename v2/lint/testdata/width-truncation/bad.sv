module top #(parameter W = 16) (input clk, input [15:0] wide, input [7:0] narrow, input int count_in,
                                 output logic [7:0] q, output logic [3:0] r, output wire [7:0] w);
  logic [7:0] acc;
  logic [W-1:0] param_wide;
  always_ff @(posedge clk) begin
    q <= wide;                   // want
    r <= {narrow, 1'b1};         // want
    acc <= acc + count_in;       // want
    acc <= param_wide;           // want
  end
  assign w = wide & 16'hFFFF;    // want
  logic [3:0] init = narrow;     // want
endmodule
